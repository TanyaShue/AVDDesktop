// Package adb 封装 adb 的必要使用部分：设备列表、等待设备上线与开机完成、优雅停止实例。
//
// 见 目标.md「六、ADB」：软件只保留检测模拟器是否已启动与获取设备状态所需的能力。
package adb

import (
	"context"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// Client 是 adb 命令封装。
type Client struct {
	AdbPath string
	Env     []string
	log     logging.Interface
}

// New 创建 adb 客户端。log 为 nil 时使用空日志器。
func New(adbPath string, env []string, log logging.Interface) *Client {
	return &Client{AdbPath: adbPath, Env: env, log: logging.Or(log)}
}

// Available 判断 adb 是否存在。
func (c *Client) Available() bool { return c.AdbPath != "" && platform.FileExists(c.AdbPath) }

// Devices 解析 `adb devices -l`。
func (c *Client) Devices(ctx context.Context) ([]domain.AdbDevice, error) {
	if !c.Available() {
		return nil, domain.Err(domain.CodeToolMissing, "未找到 adb（platform-tools 未安装）")
	}
	started := time.Now()
	out, err := proc.Output(ctx, c.AdbPath, []string{"devices", "-l"}, proc.Options{
		Env: c.Env, Timeout: 30 * time.Second,
	})
	if err != nil {
		c.log.Warn("adb", "adb devices 失败：%v", err)
		return nil, err
	}
	devices := ParseDevices(out)
	c.log.Debug("adb", "adb devices -l 返回 %d 个设备（耗时 %s）",
		len(devices), time.Since(started).Round(time.Millisecond))
	return devices, nil
}

// ParseDevices 解析 `adb devices -l` 输出。
func ParseDevices(out string) []domain.AdbDevice {
	var devices []domain.AdbDevice
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := domain.AdbDevice{Serial: fields[0], State: fields[1]}
		d.IsEmulator = strings.HasPrefix(d.Serial, "emulator-")
		for _, f := range fields[2:] {
			kv := strings.SplitN(f, ":", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "product":
				d.Product = kv[1]
			case "model":
				d.Model = kv[1]
			case "device":
				d.Device = kv[1]
			case "transport_id":
				d.TransportID = kv[1]
			}
		}
		devices = append(devices, d)
	}
	return devices
}

// SerialForPort 返回端口对应的 adb 串号（console port = 5554 → emulator-5554）。
func SerialForPort(port int) string { return "emulator-" + strconv.Itoa(port) }

// WaitForDevice 等待设备出现在 adb 列表中（有界）。
func (c *Client) WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		devices, err := c.Devices(ctx)
		if err == nil {
			for _, d := range devices {
				if d.Serial == serial && d.State == "device" {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return domain.ErrDetail(domain.CodeProcessFailed,
				"等待设备连接超时", "serial="+serial).
				WithHint("模拟器可能启动失败，请查看应用日志（模块 emulator）")
		}
		select {
		case <-ctx.Done():
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// WaitForBoot 等待系统启动完成（sys.boot_completed = 1，有界）。
func (c *Client) WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		out, err := proc.Output(ctx, c.AdbPath,
			[]string{"-s", serial, "shell", "getprop", "sys.boot_completed"},
			proc.Options{Env: c.Env, Timeout: 15 * time.Second})
		if err == nil && strings.TrimSpace(out) == "1" {
			return nil
		}
		if time.Now().After(deadline) {
			return domain.ErrDetail(domain.CodeProcessFailed,
				"等待系统启动完成超时", "serial="+serial).
				WithHint("设备已连接但系统未完成开机，请查看应用日志（模块 emulator）")
		}
		select {
		case <-ctx.Done():
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		case <-time.After(2 * time.Second):
		}
	}
}

// EmuKill 请求模拟器实例优雅退出（有界）。
func (c *Client) EmuKill(ctx context.Context, serial string) error {
	c.log.Info("adb", "请求优雅停止 %s（adb emu kill）", serial)
	_, err := proc.Output(ctx, c.AdbPath, []string{"-s", serial, "emu", "kill"}, proc.Options{
		Env: c.Env, Timeout: 20 * time.Second,
	})
	if err != nil {
		c.log.Warn("adb", "%s 的 emu kill 失败（将回退到强制结束进程）：%v", serial, err)
	}
	return err
}
