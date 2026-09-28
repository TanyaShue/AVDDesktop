package framestream

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestHeaderRoundTrip(t *testing.T) {
	f := Frame{
		Pix:       make([]byte, 4*3*4),
		Width:     4,
		Height:    3,
		Format:    FormatRGBA8888,
		Seq:       42,
		Timestamp: time.UnixMicro(1234567),
		BottomUp:  true,
	}
	raw := HeaderBytes(f)
	if len(raw) != HeaderSize {
		t.Fatalf("header size=%d", len(raw))
	}
	got, err := ParseHeader(raw[:])
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got.Width != 4 || got.Height != 3 || got.Format != FormatRGBA8888 || got.Seq != 42 {
		t.Fatalf("header=%+v", got)
	}
	if got.Flags&FlagBottomUp == 0 {
		t.Fatal("bottom-up flag missing")
	}
	if got.TimestampUnixUs != 1234567 {
		t.Fatalf("timestamp=%d", got.TimestampUnixUs)
	}
	if err := got.ValidatePayload(len(f.Pix)); err != nil {
		t.Fatalf("ValidatePayload: %v", err)
	}
}

func TestParseHeaderRejectsBadInput(t *testing.T) {
	good := HeaderBytes(Frame{Pix: make([]byte, 16), Width: 2, Height: 2, Format: FormatRGBA8888})
	if _, err := ParseHeader(good[:10]); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("short header err=%v", err)
	}
	badMagic := good
	binary.LittleEndian.PutUint32(badMagic[0:4], 0)
	if _, err := ParseHeader(badMagic[:]); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("bad magic err=%v", err)
	}
	badVersion := good
	binary.LittleEndian.PutUint16(badVersion[4:6], 99)
	if _, err := ParseHeader(badVersion[:]); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("bad version err=%v", err)
	}
}

func TestValidatePayloadRejectsMismatch(t *testing.T) {
	h := Header{Width: 2, Height: 2, Format: FormatRGBA8888, PayloadBytes: 16}
	if err := h.ValidatePayload(16); err != nil {
		t.Fatalf("valid payload: %v", err)
	}
	if err := h.ValidatePayload(15); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("length mismatch err=%v", err)
	}
	h.PayloadBytes = 15
	if err := h.ValidatePayload(15); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("size mismatch err=%v", err)
	}
}
