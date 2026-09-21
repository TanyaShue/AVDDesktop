// Package adb 封装 adb 客户端（实例状态、安装、shell、截图）。
package adb

import (
	"context"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// Client 是 adb 命令封装。
type Client struct {
	AdbPath string
	Env     []string
}

// New 创建 adb 客户端。
func New(adbPath string, env []string) *Client {
	return &Client{AdbPath: adbPath, Env: env}
}

// Available 判断 adb 是否存在。
func (c *Client) Available() bool { return c.AdbPath != "" && platform.FileExists(c.AdbPath) }

// Devices 解析 `adb devices -l`。
func (c *Client) Devices(ctx context.Context) ([]domain.AdbDevice, error) {
	if !c.Available() {
		return nil, domain.Err(domain.CodeToolMissing, "未找到 adb（platform-tools 未安装）")
	}
	out, err := proc.Output(ctx, c.AdbPath, []string{"devices", "-l"}, proc.Options{
		Env: c.Env, Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return ParseDevices(out), nil
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

// AvdName 通过 `adb -s <serial> emu avd name` 查询实例对应的 AVD 名。
func (c *Client) AvdName(ctx context.Context, serial string) (string, error) {
	out, err := proc.Output(ctx, c.AdbPath, []string{"-s", serial, "emu", "avd", "name"}, proc.Options{
		Env: c.Env, Timeout: 20 * time.Second,
	})
	if err != nil {
		return "", err
	}
	lines := platform.SplitLines(out)
	if len(lines) == 0 {
		return "", domain.Err(domain.CodeProcessFailed, "无法获取实例的 AVD 名称")
	}
	return strings.TrimSpace(lines[0]), nil
}

// WaitForDevice 等待设备出现在 adb 列表中。
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
				WithHint("模拟器可能启动失败，请查看实例日志")
		}
		select {
		case <-ctx.Done():
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// WaitForBoot 等待系统启动完成（sys.boot_completed = 1）。
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
				"等待系统启动完成超时", "serial="+serial)
		}
		select {
		case <-ctx.Done():
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		case <-time.After(2 * time.Second):
		}
	}
}

// EmuKill 请求模拟器实例优雅退出。
func (c *Client) EmuKill(ctx context.Context, serial string) error {
	_, err := proc.Output(ctx, c.AdbPath, []string{"-s", serial, "emu", "kill"}, proc.Options{
		Env: c.Env, Timeout: 20 * time.Second,
	})
	return err
}

// Shell 执行 shell 命令。
func (c *Client) Shell(ctx context.Context, serial, command string) (string, error) {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "shell", command)
	return proc.Output(ctx, c.AdbPath, args, proc.Options{Env: c.Env, Timeout: 60 * time.Second})
}

// Install 安装 APK（-r 覆盖安装，-g 授予全部权限）。
func (c *Client) Install(ctx context.Context, serial, apkPath string, grantAll bool, onLine func(string, string)) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "install", "-r")
	if grantAll {
		args = append(args, "-g")
	}
	args = append(args, apkPath)
	res, err := proc.Run(ctx, c.AdbPath, args, proc.Options{
		Env: c.Env, Timeout: 10 * time.Minute, OnLine: onLine,
	})
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(res.Stdout), "failure") {
		return domain.ErrDetail(domain.CodeProcessFailed, "APK 安装失败", res.Combined())
	}
	return nil
}

// Push 推送文件到设备。
func (c *Client) Push(ctx context.Context, serial, local, remote string, onLine func(string, string)) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "push", local, remote)
	_, err := proc.Output(ctx, c.AdbPath, args, proc.Options{
		Env: c.Env, Timeout: 30 * time.Minute, OnLine: onLine,
	})
	return err
}

// Pull 从设备拉取文件。
func (c *Client) Pull(ctx context.Context, serial, remote, local string, onLine func(string, string)) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "pull", remote, local)
	_, err := proc.Output(ctx, c.AdbPath, args, proc.Options{
		Env: c.Env, Timeout: 30 * time.Minute, OnLine: onLine,
	})
	return err
}

// Root 以 root 重启 adbd（Play 镜像会失败）。
func (c *Client) Root(ctx context.Context, serial string) (string, error) {
	return c.Shell(ctx, serial, "adb root")
}

// Remount 以可写方式重新挂载 system/vendor（需要先以 -writable-system 启动）。
func (c *Client) Remount(ctx context.Context, serial string) (string, error) {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "remount")
	return proc.Output(ctx, c.AdbPath, args, proc.Options{Env: c.Env, Timeout: 60 * time.Second})
}

// Screenshot 截屏并返回 PNG 字节。
func (c *Client) Screenshot(ctx context.Context, serial string) ([]byte, error) {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "exec-out", "screencap", "-p")
	return proc.OutputBytes(ctx, c.AdbPath, args, proc.Options{Env: c.Env, Timeout: 60 * time.Second})
}

// StartServer 启动 adb 服务。
func (c *Client) StartServer(ctx context.Context) error {
	_, err := proc.Output(ctx, c.AdbPath, []string{"start-server"}, proc.Options{
		Env: c.Env, Timeout: 60 * time.Second,
	})
	return err
}

// KillServer 关闭 adb 服务（用于重置异常状态）。
func (c *Client) KillServer(ctx context.Context) error {
	_, err := proc.Output(ctx, c.AdbPath, []string{"kill-server"}, proc.Options{
		Env: c.Env, Timeout: 30 * time.Second,
	})
	return err
}
