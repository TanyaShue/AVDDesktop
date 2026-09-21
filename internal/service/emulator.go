package service

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
)

// EmulatorService 管理模拟器实例。
type EmulatorService struct{ rt *Runtime }

// NewEmulatorService 创建 EmulatorService。
func NewEmulatorService(rt *Runtime) *EmulatorService { return &EmulatorService{rt: rt} }

// StartRequest 是启动请求。
type StartRequest struct {
	AvdName string               `json:"avdName"`
	Options domain.LaunchOptions `json:"options"`
}

// Start 启动模拟器实例。
func (s *EmulatorService) Start(req StartRequest) (*domain.EmulatorInstance, error) {
	comp := s.rt.Components()
	if req.AvdName == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "未指定设备名称")
	}
	if !platform.FileExists(comp.Paths.EmulatorExe) {
		return nil, domain.Err(domain.CodeToolMissing, "尚未安装模拟器（emulator）").
			WithAction("install", "安装模拟器", "emulator")
	}
	inst, err := comp.Launcher.Start(s.rt.Context(), req.AvdName, req.Options)
	if err != nil {
		return nil, err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "started", "name": req.AvdName})
	return inst, nil
}

// Stop 停止实例。
func (s *EmulatorService) Stop(instanceID string, force bool) error {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	if err := comp.Launcher.Stop(s.rt.Context(), instanceID, force); err != nil {
		return err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "stopped", "name": inst.AvdName})
	return nil
}

// StopByAvd 按 AVD 名称停止实例。
func (s *EmulatorService) StopByAvd(avdName string, force bool) error {
	comp := s.rt.Components()
	if err := comp.Launcher.StopByAvd(s.rt.Context(), avdName, force); err != nil {
		return err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "stopped", "name": avdName})
	return nil
}

// Restart 重启实例（停止后以新参数启动）。
func (s *EmulatorService) Restart(instanceID string, opts domain.LaunchOptions) (*domain.EmulatorInstance, error) {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	if err := comp.Launcher.Stop(s.rt.Context(), instanceID, false); err != nil {
		return nil, err
	}
	if opts.Port == 0 {
		opts.Port = inst.Port
	}
	return s.Start(StartRequest{AvdName: inst.AvdName, Options: opts})
}

// ListRunning 返回所有实例（含已停止的历史记录）。
func (s *EmulatorService) ListRunning() []domain.EmulatorInstance {
	comp := s.rt.Components()
	items := comp.Launcher.List()
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt > items[j].StartedAt })
	return items
}

// GetLog 返回实例日志。
func (s *EmulatorService) GetLog(instanceID string, tail int) ([]domain.LogLine, error) {
	comp := s.rt.Components()
	if _, ok := comp.Launcher.Get(instanceID); !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	return comp.Launcher.Logs(instanceID, tail), nil
}

// Screenshot 返回实例截图的 base64 PNG（data URL 形式）。
func (s *EmulatorService) Screenshot(instanceID string) (string, error) {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return "", domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	if inst.State != domain.AvdRunning {
		return "", domain.Err(domain.CodeInvalidArgument, "实例尚未启动完成，无法截图")
	}
	data, err := comp.Adb.Screenshot(s.rt.Context(), inst.Serial)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", domain.Err(domain.CodeProcessFailed, "截图返回空数据")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// SendKey 发送按键（音量/电源/返回等）。
func (s *EmulatorService) SendKey(instanceID, key string) error {
	code, ok := keyCodes[strings.ToUpper(strings.TrimSpace(key))]
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "不支持的按键: "+key)
	}
	return s.shell(instanceID, "input keyevent "+code)
}

// Rotate 旋转屏幕。
func (s *EmulatorService) Rotate(instanceID, orientation string) error {
	rotation := map[string]string{"portrait": "0", "landscape": "1", "reverse-portrait": "2", "reverse-landscape": "3"}
	value, ok := rotation[strings.ToLower(strings.TrimSpace(orientation))]
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "方向必须是 portrait/landscape/reverse-portrait/reverse-landscape")
	}
	if err := s.shell(instanceID, "settings put system accelerometer_rotation 0"); err != nil {
		return err
	}
	return s.shell(instanceID, "settings put system user_rotation "+value)
}

// SetGps 设置模拟位置。
func (s *EmulatorService) SetGps(instanceID string, lat, lon float64) error {
	return s.emu(instanceID, "geo fix "+strconv.FormatFloat(lon, 'f', 6, 64)+" "+strconv.FormatFloat(lat, 'f', 6, 64))
}

// SetNetwork 设置网络速度与延迟（弱网测试）。
func (s *EmulatorService) SetNetwork(instanceID, speed, delay string) error {
	if speed != "" {
		if err := s.emu(instanceID, "network speed "+speed); err != nil {
			return err
		}
	}
	if delay != "" {
		if err := s.emu(instanceID, "network delay "+delay); err != nil {
			return err
		}
	}
	return nil
}

// SetBattery 设置电池状态。
func (s *EmulatorService) SetBattery(instanceID string, level int, charging bool) error {
	if level < 0 || level > 100 {
		return domain.Err(domain.CodeInvalidArgument, "电量必须在 0-100 之间")
	}
	if err := s.emu(instanceID, "power capacity "+strconv.Itoa(level)); err != nil {
		return err
	}
	status := "discharging"
	if charging {
		status = "charging"
	}
	return s.emu(instanceID, "power ac "+(map[bool]string{true: "on", false: "off"})[charging]+
		" ; power status "+status)
}

// Snapshot 通过扫描 snapshots 目录列出快照。
//
// 注意：快照的创建/恢复由 emulator 的 `-snapshot` 参数与运行期控制台完成，
// 这里只做只读列表（写操作见 TODO）。
func (s *EmulatorService) Snapshots(avdName string) ([]domain.Snapshot, error) {
	comp := s.rt.Components()
	dir := filepath.Join(comp.Store.Resolve(avdName).Dir, "snapshots")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // 没有快照目录是正常状态
	}
	var out []domain.Snapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var size int64
		_ = filepath.Walk(filepath.Join(dir, e.Name()), func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				size += info.Size()
			}
			return nil
		})
		out = append(out, domain.Snapshot{Name: e.Name(), SizeBytes: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DeleteSnapshot 删除快照目录。
func (s *EmulatorService) DeleteSnapshot(avdName, name string) error {
	comp := s.rt.Components()
	if _, ok := comp.Launcher.ByAvd(avdName); ok {
		return domain.Err(domain.CodeFileInUse, "设备正在运行，无法删除快照")
	}
	target := filepath.Join(comp.Store.Resolve(avdName).Dir, "snapshots", name)
	if !platform.DirExists(target) {
		return domain.Err(domain.CodeInvalidArgument, "快照不存在: "+name)
	}
	if err := os.RemoveAll(target); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法删除快照", err)
	}
	return nil
}

// SaveSnapshot 保存快照（通过模拟器控制台命令）。
func (s *EmulatorService) SaveSnapshot(avdName, name string) error {
	inst, ok := s.rt.Components().Launcher.ByAvd(avdName)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "该设备没有运行中的实例，请先启动")
	}
	return s.emu(inst.ID, "avd snapshot save "+name)
}

// LoadSnapshot 加载快照。
func (s *EmulatorService) LoadSnapshot(avdName, name string) error {
	inst, ok := s.rt.Components().Launcher.ByAvd(avdName)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "该设备没有运行中的实例，请先启动")
	}
	return s.emu(inst.ID, "avd snapshot load "+name)
}

// Shutdown 停止所有实例（应用退出）。
func (s *EmulatorService) Shutdown(force bool) {
	s.rt.Components().Launcher.StopAll(context.Background(), force)
}

func (s *EmulatorService) shell(instanceID, command string) error {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	if inst.State != domain.AvdRunning {
		return domain.Err(domain.CodeInvalidArgument, "实例尚未启动完成")
	}
	_, err := comp.Adb.Shell(s.rt.Context(), inst.Serial, command)
	return err
}

// emu 发送模拟器控制台命令（通过 adb emu 转发）。
func (s *EmulatorService) emu(instanceID, consoleCommand string) error {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	args := strings.Fields(consoleCommand)
	full := append([]string{"-s", inst.Serial, "emu"}, args...)
	_, err := outputCommand(s.rt.Context(), comp.Paths.Adb, full, comp.Env)
	return err
}

var keyCodes = map[string]string{
	"POWER":           "26",
	"BACK":            "4",
	"HOME":            "3",
	"MENU":            "82",
	"APP_SWITCH":      "187",
	"VOLUME_UP":       "24",
	"VOLUME_DOWN":     "25",
	"MUTE":            "164",
	"ENTER":           "66",
	"DEL":             "67",
	"CAMERA":          "27",
	"WAKEUP":          "224",
	"SLEEP":           "223",
	"ROTATE":          "293",
	"BRIGHTNESS_UP":   "221",
	"BRIGHTNESS_DOWN": "220",
}

// ---------------------------------------------------------------- AdbService

// AdbService 提供 adb 相关操作（设备列表、安装 APK、shell）。
type AdbService struct{ rt *Runtime }

// NewAdbService 创建 AdbService。
func NewAdbService(rt *Runtime) *AdbService { return &AdbService{rt: rt} }

// Devices 返回 adb 设备列表，并尽可能关联到已知实例。
func (s *AdbService) Devices() ([]domain.AdbDevice, error) {
	comp := s.rt.Components()
	devices, err := comp.Adb.Devices(s.rt.Context())
	if err != nil {
		return nil, err
	}
	bySerial := map[string]string{}
	for _, inst := range comp.Launcher.List() {
		bySerial[inst.Serial] = inst.ID
	}
	for i := range devices {
		if id, ok := bySerial[devices[i].Serial]; ok {
			devices[i].InstanceID = id
		}
	}
	return devices, nil
}

// InstallApkRequest 是安装 APK 请求。
type InstallApkRequest struct {
	Serial   string `json:"serial"`
	ApkPath  string `json:"apkPath"`
	GrantAll bool   `json:"grantAll"`
}

// InstallApk 安装 APK，返回 jobID。
func (s *AdbService) InstallApk(req InstallApkRequest) (string, error) {
	if !platform.FileExists(req.ApkPath) {
		return "", domain.Err(domain.CodePathNotFound, "APK 文件不存在: "+req.ApkPath)
	}
	comp := s.rt.Components()
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobInstall,
		Title: "安装 APK " + filepath.Base(req.ApkPath),
	}, func(ctx context.Context, j *job.Job) error {
		j.SetPhase("正在安装到 " + req.Serial)
		return comp.Adb.Install(ctx, req.Serial, req.ApkPath, req.GrantAll, func(stream, line string) {
			j.Log("info", "adb", line)
		})
	})
	return j.ID(), nil
}

// PushRequest 是文件推送请求。
type PushRequest struct {
	Serial string `json:"serial"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// Push 推送文件到设备，返回 jobID。
func (s *AdbService) Push(req PushRequest) (string, error) {
	if !platform.FileExists(req.Local) && !platform.DirExists(req.Local) {
		return "", domain.Err(domain.CodePathNotFound, "本地路径不存在: "+req.Local)
	}
	comp := s.rt.Components()
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobInstall,
		Title: "推送文件到 " + req.Serial,
	}, func(ctx context.Context, j *job.Job) error {
		return comp.Adb.Push(ctx, req.Serial, req.Local, nonEmpty(req.Remote, "/sdcard/Download/"),
			func(stream, line string) { j.Log("info", "adb", line) })
	})
	return j.ID(), nil
}

// PullRequest 是文件拉取请求。
type PullRequest struct {
	Serial string `json:"serial"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

// Pull 从设备拉取文件，返回 jobID。
func (s *AdbService) Pull(req PullRequest) (string, error) {
	comp := s.rt.Components()
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobInstall,
		Title: "从设备拉取文件",
	}, func(ctx context.Context, j *job.Job) error {
		return comp.Adb.Pull(ctx, req.Serial, req.Remote, req.Local,
			func(stream, line string) { j.Log("info", "adb", line) })
	})
	return j.ID(), nil
}

// Shell 执行 adb shell 命令。
func (s *AdbService) Shell(serial, command string) (string, error) {
	comp := s.rt.Components()
	return comp.Adb.Shell(s.rt.Context(), serial, command)
}

// Root 尝试以 root 重启 adbd。
func (s *AdbService) Root(serial string) (string, error) {
	comp := s.rt.Components()
	out, err := comp.Adb.Shell(s.rt.Context(), serial, "id")
	if err == nil && strings.Contains(out, "uid=0") {
		return "已经是 root 权限", nil
	}
	_, err = outputCommand(s.rt.Context(), comp.Paths.Adb, []string{"-s", serial, "root"}, comp.Env)
	if err != nil {
		return "", domain.ErrDetail(domain.CodeProcessFailed, "无法切换到 root",
			"Play 商店镜像不支持 root，请改用 google_apis 或 aosp 镜像")
	}
	return "已请求切换到 root", nil
}

// Remount 以可写方式重新挂载系统分区。
func (s *AdbService) Remount(serial string) (string, error) {
	comp := s.rt.Components()
	return comp.Adb.Remount(s.rt.Context(), serial)
}

// KillServer 关闭 adb 服务。
func (s *AdbService) KillServer() error {
	comp := s.rt.Components()
	return comp.Adb.KillServer(s.rt.Context())
}

// StartServer 启动 adb 服务。
func (s *AdbService) StartServer() error {
	comp := s.rt.Components()
	return comp.Adb.StartServer(s.rt.Context())
}

// LogcatRequest 是日志订阅请求。
type LogcatRequest struct {
	Serial string `json:"serial"`
	Filter string `json:"filter"`
}

// StartLogcat 启动日志流（持续推送事件）。
//
// TODO(M6): 目前只验证设备可用性并返回提示；完整实现需要长驻进程 + 行流式事件。
func (s *AdbService) StartLogcat(req LogcatRequest) (string, error) {
	comp := s.rt.Components()
	if _, err := outputCommand(context.Background(), comp.Paths.Adb,
		[]string{"-s", req.Serial, "get-state"}, comp.Env); err != nil {
		return "", err
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobLogcat,
		Title: "logcat " + req.Serial,
	}, func(ctx context.Context, j *job.Job) error {
		j.Log("info", "logcat", "日志流功能将在 M6 完成（当前占位）")
		return nil
	})
	return j.ID(), nil
}

func (s *AdbService) adbPath() string { return s.rt.Components().Paths.Adb }

var _ = time.Second
