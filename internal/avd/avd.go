// Package avd 管理软件自有的 AVD 主目录：列出 / 读取 / 删除设备，
// 并通过官方 avdmanager 创建设备、列出设备档案。
//
// 只处理“软件自己的 avd 目录里的 ini 文件”，不实现 avdmanager 已有的能力。
package avd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// Store 提供 AVD 目录的读写能力。
type Store struct {
	AvdHome string
	// SdkRoot 用于校验 config.ini 里的系统镜像是否仍然存在（为空时跳过校验）。
	SdkRoot string
}

// New 创建 Store。
func New(avdHome, sdkRoot string) *Store {
	return &Store{AvdHome: avdHome, SdkRoot: sdkRoot}
}

// Layout 描述一个 AVD 的磁盘布局。
type Layout struct {
	Name      string // AVD 名称（.ini 文件名去掉后缀）
	IniPath   string // <avdHome>/<Name>.ini
	Dir       string // 真实目录（以 .ini 里的 path 为准）
	ConfigIni string // <Dir>/config.ini
	Exists    bool
}

// Resolve 解析指定名称的布局。
//
// 注意（实测结论）：.ini 文件名不保证等于 .avd 目录名。
// 样本：Medium_Phone_API_36.1.ini 里 path=…\avd\Medium_Phone.avd。
// 因此必须先读 .ini 的 path 字段，读不到再退化为 <name>.avd。
func (s *Store) Resolve(name string) Layout {
	iniPath := filepath.Join(s.AvdHome, name+".ini")
	dir := ""
	if data, err := os.ReadFile(iniPath); err == nil {
		cfg := parseIni(string(data))
		if p := strings.TrimSpace(cfg["path"]); p != "" {
			dir = p
		} else if rel := strings.TrimSpace(cfg["path.rel"]); rel != "" {
			dir = filepath.Join(s.AvdHome, rel)
		}
	}
	switch {
	case dir == "":
		dir = filepath.Join(s.AvdHome, name+".avd")
	case !filepath.IsAbs(dir):
		dir = filepath.Join(s.AvdHome, dir)
	}
	dir = filepath.Clean(dir)
	return Layout{
		Name:      name,
		IniPath:   iniPath,
		Dir:       dir,
		ConfigIni: filepath.Join(dir, "config.ini"),
		Exists:    platform.DirExists(dir),
	}
}

// Exists 判断 AVD 是否存在（目录或 .ini 任一存在）。
func (s *Store) Exists(name string) bool {
	l := s.Resolve(name)
	return l.Exists || platform.FileExists(l.IniPath)
}

// List 扫描 AVD 主目录，返回所有设备摘要（按名称排序）。
//
// 单个设备配置损坏不会影响其它设备的展示，只在该条上标记 Broken。
func (s *Store) List() ([]domain.AvdSummary, error) {
	entries, err := os.ReadDir(s.AvdHome)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, domain.Wrap(domain.CodePathNotFound, "无法读取 AVD 目录: "+s.AvdHome, err)
	}
	var out []domain.AvdSummary
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".ini") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		summary, err := s.summary(name)
		if err != nil {
			out = append(out, domain.AvdSummary{
				Name:   name,
				Path:   filepath.Join(s.AvdHome, name+".avd"),
				State:  domain.AvdStopped,
				Broken: err.Error(),
			})
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadConfig 读取设备的 config.ini 键值对。
func (s *Store) ReadConfig(name string) (map[string]string, error) {
	l := s.Resolve(name)
	data, err := os.ReadFile(l.ConfigIni)
	if err != nil {
		return nil, domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	return parseIni(string(data)), nil
}

// Delete 删除设备：同时删除 .avd 目录与 .ini 文件。
func (s *Store) Delete(name string) error {
	l := s.Resolve(name)
	if !l.Exists && !platform.FileExists(l.IniPath) {
		return domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	if l.Exists {
		if err := os.RemoveAll(l.Dir); err != nil {
			return domain.Wrap(domain.CodePermissionDenied,
				"无法删除设备目录（可能被运行中的模拟器占用）", err).
				WithHint("请先停止该设备的模拟器实例")
		}
	}
	if platform.FileExists(l.IniPath) {
		if err := os.Remove(l.IniPath); err != nil {
			return domain.Wrap(domain.CodePermissionDenied, "无法删除 .ini 文件", err)
		}
	}
	return nil
}

// ValidateName 校验设备名称并给出可用建议。
func (s *Store) ValidateName(name string) domain.NameValidation {
	res := domain.NameValidation{Name: name}
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		res.Reason = "名称不能为空"
	case !nameRe.MatchString(trimmed):
		res.Reason = "只能包含字母、数字、下划线、点和连字符，长度 1-64"
	case s.Exists(trimmed):
		res.Reason = "已存在同名设备"
		res.Suggest = s.suggestName(trimmed)
	default:
		res.Valid = true
	}
	return res
}

func (s *Store) suggestName(base string) string {
	for i := 2; i < 100; i++ {
		candidate := base + "_" + itoa(i)
		if !s.Exists(candidate) {
			return candidate
		}
	}
	return base + "_copy"
}

// summary 读取单个设备的摘要。
func (s *Store) summary(name string) (domain.AvdSummary, error) {
	l := s.Resolve(name)
	if !l.Exists {
		return domain.AvdSummary{}, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	config, err := s.ReadConfig(name)
	if err != nil {
		return domain.AvdSummary{}, err
	}
	sum := domain.AvdSummary{
		Name:            name,
		Path:            l.Dir,
		API:             strings.TrimPrefix(config["target"], "android-"),
		Tag:             firstNonEmpty(config["tag.id"], firstTagID(config["tag.ids"])),
		ABI:             firstNonEmpty(config["abi.type"], config["hw.cpu.arch"]),
		DeviceProfileID: config["hw.device.name"],
		State:           domain.AvdStopped,
	}
	// 校验系统镜像是否仍然存在（仅在已知 SDK 根目录时校验）
	sysDir := config["image.sysdir.1"]
	switch {
	case sysDir == "":
		sum.Broken = "缺少 image.sysdir.1，配置不完整"
	case s.SdkRoot != "" && !platform.DirExists(filepath.Join(s.SdkRoot, sysDir)):
		sum.Broken = "系统镜像已不存在：" + sysDir
	}
	return sum, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// parseIni 解析 ini 文本（AVD 的 ini 不使用 section，只取 key=value）。
func parseIni(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		i := strings.IndexByte(trimmed, '=')
		if i <= 0 {
			continue
		}
		out[strings.TrimSpace(trimmed[:i])] = strings.TrimSpace(trimmed[i+1:])
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstTagID(tagIDs string) string {
	if tagIDs == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(tagIDs, ",")[0])
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := [20]byte{}
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}
