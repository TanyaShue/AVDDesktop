// Package framestream 提供设备画面原始帧的本机传输能力：
// 生产者把 RGBA 帧交给 Hub，WebView 通过 WebSocket 订阅二进制帧，
// Hub 只保留最新帧并对慢订阅者丢帧，绝不反压生产者。
package framestream

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	// HeaderSize 是每帧二进制消息固定头长度。
	HeaderSize = 32

	// magic 是 ASCII "AVDF" 的小端 uint32，用于快速识别帧消息。
	magic = uint32(0x46445641)

	// Version 是当前协议版本。
	Version = uint16(1)
)

// Format 是帧负载的像素格式。
type Format uint16

const (
	FormatRGBA8888 Format = 0
	FormatRGB888   Format = 1
	FormatJPEG     Format = 2
)

// FlagBottomUp 表示像素行序自底向上；模拟器实测为 top-down，默认不设置。
const FlagBottomUp uint16 = 1 << 0

// ErrInvalidHeader 表示收到的二进制消息不是合法的设备帧头。
var ErrInvalidHeader = errors.New("invalid frame header")

// Frame 是一帧待发布的画面。
//
// Hub 取得 Pix 的所有权；当所有订阅者都消费完（或帧被新帧覆盖）后调用 Release。
// Release 允许为 nil。生产者调用 Publish 后不得再读写 Pix。
type Frame struct {
	Pix       []byte
	Width     int
	Height    int
	Format    Format
	Seq       uint32
	Timestamp time.Time
	BottomUp  bool
	Release   func()
}

// Header 是解析后的帧头。
type Header struct {
	Version         uint16
	Flags           uint16
	Width           int
	Height          int
	Format          Format
	Seq             uint32
	TimestampUnixUs int64
	PayloadBytes    int
}

// BytesPerPixel 返回像素格式的每像素字节数；未知/JPEG 返回 0。
func (f Format) BytesPerPixel() int {
	switch f {
	case FormatRGBA8888:
		return 4
	case FormatRGB888:
		return 3
	default:
		return 0
	}
}

// ExpectedPayload 返回给定尺寸与格式下的期望负载长度；JPEG 无法从尺寸推断，返回 0。
func ExpectedPayload(format Format, width, height int) int {
	bpp := format.BytesPerPixel()
	if bpp == 0 || width <= 0 || height <= 0 {
		return 0
	}
	return width * height * bpp
}

// HeaderBytes 把帧元数据编码为固定 32 字节头（小端序）。
func HeaderBytes(f Frame) [HeaderSize]byte {
	var out [HeaderSize]byte
	binary.LittleEndian.PutUint32(out[0:4], magic)
	binary.LittleEndian.PutUint16(out[4:6], Version)
	var flags uint16
	if f.BottomUp {
		flags |= FlagBottomUp
	}
	binary.LittleEndian.PutUint16(out[6:8], flags)
	binary.LittleEndian.PutUint16(out[8:10], uint16(f.Width))
	binary.LittleEndian.PutUint16(out[10:12], uint16(f.Height))
	binary.LittleEndian.PutUint16(out[12:14], uint16(f.Format))
	binary.LittleEndian.PutUint16(out[14:16], 0)
	binary.LittleEndian.PutUint32(out[16:20], f.Seq)
	var ts int64
	if !f.Timestamp.IsZero() {
		ts = f.Timestamp.UnixMicro()
	}
	binary.LittleEndian.PutUint64(out[20:28], uint64(ts))
	binary.LittleEndian.PutUint32(out[28:32], uint32(len(f.Pix)))
	return out
}

// ParseHeader 解析并校验固定帧头；只校验协议层字段，负载长度由调用方校验。
func ParseHeader(data []byte) (Header, error) {
	if len(data) < HeaderSize {
		return Header{}, fmt.Errorf("%w: need %d bytes, got %d", ErrInvalidHeader, HeaderSize, len(data))
	}
	if got := binary.LittleEndian.Uint32(data[0:4]); got != magic {
		return Header{}, fmt.Errorf("%w: magic=0x%08x", ErrInvalidHeader, got)
	}
	version := binary.LittleEndian.Uint16(data[4:6])
	if version != Version {
		return Header{}, fmt.Errorf("%w: version=%d", ErrInvalidHeader, version)
	}
	h := Header{
		Version:         version,
		Flags:           binary.LittleEndian.Uint16(data[6:8]),
		Width:           int(binary.LittleEndian.Uint16(data[8:10])),
		Height:          int(binary.LittleEndian.Uint16(data[10:12])),
		Format:          Format(binary.LittleEndian.Uint16(data[12:14])),
		Seq:             binary.LittleEndian.Uint32(data[16:20]),
		TimestampUnixUs: int64(binary.LittleEndian.Uint64(data[20:28])),
		PayloadBytes:    int(binary.LittleEndian.Uint32(data[28:32])),
	}
	if h.Width <= 0 || h.Height <= 0 {
		return Header{}, fmt.Errorf("%w: invalid size %dx%d", ErrInvalidHeader, h.Width, h.Height)
	}
	if h.PayloadBytes < 0 {
		return Header{}, fmt.Errorf("%w: negative payload", ErrInvalidHeader)
	}
	return h, nil
}

// ValidatePayload 校验负载长度是否与帧头一致，并在像素格式可推断时校验尺寸一致性。
func (h Header) ValidatePayload(payload int) error {
	if payload != h.PayloadBytes {
		return fmt.Errorf("%w: payload=%d want=%d", ErrInvalidHeader, payload, h.PayloadBytes)
	}
	if expected := ExpectedPayload(h.Format, h.Width, h.Height); expected > 0 && payload != expected {
		return fmt.Errorf("%w: payload=%d want=%d for format %d", ErrInvalidHeader, payload, expected, h.Format)
	}
	return nil
}

// ReleaseFrame 调用帧的释放回调（允许 nil）。
//
// 供 Hub 之外的兜底路径使用：例如 Hub 未挂载、会话已关闭时，调用方仍应把帧缓冲归还帧池。
func ReleaseFrame(f Frame) {
	release(f)
}

// release 调用帧的释放回调（允许 nil）。
func release(f Frame) {
	if f.Release != nil {
		f.Release()
	}
}
