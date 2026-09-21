package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// MetaFileName 是我们写在 <name>.avd/ 下的项目元数据文件（不污染 config.ini）。
const MetaFileName = "avddesktop.json"

// Meta 是 AVD 的项目级元数据。
type Meta struct {
	Name         string                `json:"name"`
	DisplayName  string                `json:"displayName,omitempty"`
	CreatedAt    int64                 `json:"createdAt,omitempty"`
	UpdatedAt    int64                 `json:"updatedAt,omitempty"`
	LastUsedAt   int64                 `json:"lastUsedAt,omitempty"`
	SourceID     string                `json:"sourceId,omitempty"`  // 创建时使用的镜像源
	SourceURL    string                `json:"sourceUrl,omitempty"` // 镜像源地址
	ProfileID    string                `json:"profileId,omitempty"` // 设备档案 id
	CreatedBy    string                `json:"createdBy,omitempty"` // avdmanager | direct
	Tags         []string              `json:"tags,omitempty"`
	Note         string                `json:"note,omitempty"`
	LaunchPreset *domain.LaunchOptions `json:"launchPreset,omitempty"`
	Extra        map[string]string     `json:"extra,omitempty"`
}

// Store 提供 AVD 目录的读写能力。
type Store struct {
	AvdHome string
	// SdkRoot 用于校验 config.ini 里的 image.sysdir.1 是否仍存在（可为空，为空时跳过校验）。
	SdkRoot string
}

// New 创建 Store。
func New(avdHome string) *Store { return &Store{AvdHome: avdHome} }

// SetSdkRoot 设置 SDK 根目录（用于校验系统镜像是否存在）。
func (s *Store) SetSdkRoot(root string) *Store {
	s.SdkRoot = root
	return s
}

// Layout 描述一个 AVD 的磁盘布局。
type Layout struct {
	Name      string // AVD 名称（去掉 .ini 后缀）
	IniPath   string // <avdHome>/<Name>.ini
	Dir       string // <avdHome>/<Name>.avd
	ConfigIni string // <avdHome>/<Name>.avd/config.ini
	MetaPath  string // <avdHome>/<Name>.avd/avddesktop.json
	Exists    bool
}

// Resolve 解析指定名称的布局。
//
// 注意（实测发现）：.ini 文件名不保证等于 .avd 目录名。
// 本机样本：Medium_Phone_API_36.1.ini 里 path=C:\Android\.android\avd\Medium_Phone.avd。
// 因此必须先读 .ini 的 path 字段，读不到再退化为 <name>.avd。
func (s *Store) Resolve(name string) Layout {
	iniPath := filepath.Join(s.AvdHome, name+".ini")
	dir := ""
	if data, err := os.ReadFile(iniPath); err == nil {
		cfg := ParseIni(string(data))
		if p := strings.TrimSpace(cfg["path"]); p != "" {
			dir = p
		} else if rel := strings.TrimSpace(cfg["path.rel"]); rel != "" {
			dir = filepath.Join(s.AvdHome, rel)
		}
	}
	if dir == "" {
		dir = filepath.Join(s.AvdHome, name+".avd")
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.AvdHome, dir)
	}
	dir = filepath.Clean(dir)
	return Layout{
		Name:      name,
		IniPath:   iniPath,
		Dir:       dir,
		ConfigIni: filepath.Join(dir, "config.ini"),
		MetaPath:  filepath.Join(dir, MetaFileName),
		Exists:    platform.DirExists(dir),
	}
}

// List 扫描 AVD 主目录，返回所有设备摘要。
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
		summary, err := s.Summary(name)
		if err != nil {
			// 单个 AVD 损坏不影响其它设备展示
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

// Names 返回所有 AVD 名称。
func (s *Store) Names() []string {
	items, _ := s.List()
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

// Exists 判断 AVD 是否存在。
func (s *Store) Exists(name string) bool {
	l := s.Resolve(name)
	return platform.DirExists(l.Dir) || platform.FileExists(l.IniPath)
}

// Summary 读取单个 AVD 的摘要信息。
func (s *Store) Summary(name string) (domain.AvdSummary, error) {
	l := s.Resolve(name)
	if !l.Exists {
		return domain.AvdSummary{}, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	config, err := ReadIni(l.ConfigIni)
	if err != nil {
		return domain.AvdSummary{}, domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	meta, _ := s.ReadMeta(name)

	sum := domain.AvdSummary{
		Name:              name,
		DisplayName:       firstNonEmpty(config["avd.ini.displayname"], metaDisplay(meta), name),
		Path:              l.Dir,
		Target:            config["target"],
		APILevel:          strings.TrimPrefix(config["target"], "android-"),
		TagID:             firstNonEmpty(config["tag.id"], firstTagID(config["tag.ids"])),
		TagDisplay:        firstNonEmpty(config["tag.display"], config["tag.displaynames"], config["tag.id"]),
		ABI:               firstNonEmpty(config["abi.type"], config["hw.cpu.arch"]),
		DeviceProfileID:   config["hw.device.name"],
		DeviceProfileName: config["hw.device.name"],
		OEM:               config["hw.device.manufacturer"],
		RAMMB:             atoi(config["hw.ramSize"]),
		Cores:             atoi(config["hw.cpu.ncore"]),
		DataPartition:     config["disk.dataPartition.size"],
		SDCard:            config["sdcard.size"],
		Width:             atoi(config["hw.lcd.width"]),
		Height:            atoi(config["hw.lcd.height"]),
		Density:           atoi(config["hw.lcd.density"]),
		GPUEnabled:        eqYes(config["hw.gpu.enabled"]),
		GPUMode:           config["hw.gpu.mode"],
		Playstore:         eqYes(getCI(config, "PlayStore.enabled")),
		State:             domain.AvdStopped,
		SizeBytes:         platform.DirSize(l.Dir),
	}
	if meta != nil {
		sum.CreatedAt = meta.CreatedAt
		sum.LastUsedAt = meta.LastUsedAt
		sum.Tags = meta.Tags
		sum.Note = meta.Note
		sum.DisplayName = firstNonEmpty(config["avd.ini.displayname"], meta.DisplayName, name)
		if meta.ProfileID != "" {
			sum.DeviceProfileID = meta.ProfileID
		}
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

// Detail 读取完整详情（含原始 config.ini）。
func (s *Store) Detail(name string) (domain.AvdDetail, error) {
	l := s.Resolve(name)
	sum, err := s.Summary(name)
	if err != nil {
		return domain.AvdDetail{}, err
	}
	config, err := ReadIni(l.ConfigIni)
	if err != nil {
		return domain.AvdDetail{}, domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	raw, _ := os.ReadFile(l.ConfigIni)
	detail := domain.AvdDetail{
		Summary:        sum,
		Config:         config,
		RawConfig:      string(raw),
		SystemImageDir: filepath.Join(s.SdkRoot, config["image.sysdir.1"]),
	}
	if s.SdkRoot != "" {
		detail.MissingImage = !platform.DirExists(detail.SystemImageDir)
	}
	return detail, nil
}

// ReadIni 解析 ini 文件为键值对（保留原始键名大小写）。
func ReadIni(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseIni(string(data)), nil
}

// ParseIni 解析 ini 文本。
func ParseIni(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			continue // AVD 的 config.ini 不使用 section
		}
		i := strings.IndexByte(trimmed, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:i])
		val := strings.TrimSpace(trimmed[i+1:])
		out[key] = val
	}
	return out
}

// WriteIni 写出 ini 文件（保留基础键的顺序，其余按键名排序，便于 diff 与人工阅读）。
func WriteIni(path string, values map[string]string) error {
	content := FormatIni(values)
	return platform.WriteFileAtomic(path, []byte(content), 0o644)
}

// FormatIni 把键值对格式化为 ini 文本。
func FormatIni(values map[string]string) string {
	priority := []string{
		"AvdId", "PlayStore.enabled", "abi.type", "avd.ini.displayname", "avd.ini.encoding",
		"image.sysdir.1", "target", "tag.id", "tag.ids", "tag.display", "tag.displaynames",
	}
	var b strings.Builder
	written := map[string]bool{}
	for _, k := range priority {
		if v, ok := values[k]; ok {
			b.WriteString(k + "=" + v + "\n")
			written[k] = true
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if !written[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k + "=" + values[k] + "\n")
	}
	return b.String()
}

// ApplyHW 把硬件覆盖项合并进 config（跳过空值）。
func ApplyHW(config map[string]string, hw map[string]string) {
	for k, v := range hw {
		if strings.TrimSpace(v) == "" {
			continue
		}
		config[k] = v
	}
}

// DiffConfig 计算修改前后的差异（用于"ini 直编"保存确认）。
func DiffConfig(before, after map[string]string) domain.ConfigDiff {
	diff := domain.ConfigDiff{Added: map[string]string{}, Changed: map[string]string{}}
	for k, v := range after {
		old, ok := before[k]
		switch {
		case !ok:
			diff.Added[k] = v
		case old != v:
			diff.Changed[k] = v
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			diff.Removed = append(diff.Removed, k)
		}
	}
	sort.Strings(diff.Removed)
	schema := SchemaByKey()
	for k := range diff.Added {
		if _, ok := schema[k]; !ok {
			diff.Warnings = append(diff.Warnings, "未知配置项（可能无效）: "+k)
		}
	}
	for k := range diff.Changed {
		if _, ok := schema[k]; !ok {
			diff.Warnings = append(diff.Warnings, "未知配置项（可能无效）: "+k)
		}
	}
	sort.Strings(diff.Warnings)
	return diff
}

// Meta 读写

// ReadMeta 读取项目元数据（不存在时返回 nil）。
func (s *Store) ReadMeta(name string) (*Meta, error) {
	l := s.Resolve(name)
	var m Meta
	if err := platform.ReadJSON(l.MetaPath, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteMeta 写入项目元数据。
func (s *Store) WriteMeta(name string, m *Meta) error {
	l := s.Resolve(name)
	if m.UpdatedAt == 0 {
		m.UpdatedAt = platform.NowMs()
	}
	return platform.WriteJSON(l.MetaPath, m)
}

// TouchLastUsed 更新最近使用时间（启动/停止时调用）。
func (s *Store) TouchLastUsed(name string) {
	m, err := s.ReadMeta(name)
	if err != nil || m == nil {
		m = &Meta{Name: name, CreatedAt: platform.NowMs()}
	}
	m.LastUsedAt = platform.NowMs()
	m.UpdatedAt = m.LastUsedAt
	_ = s.WriteMeta(name, m)
}

// ---------------------------------------------------------------- 名称校验

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidateName 校验 AVD 名称并给出可用建议。
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

// ---------------------------------------------------------------- 删除 / 克隆

// Delete 删除 AVD；withFiles 为 true 时同时删除 .avd 目录与 .ini。
func (s *Store) Delete(name string, withFiles bool) error {
	l := s.Resolve(name)
	if !l.Exists && !platform.FileExists(l.IniPath) {
		return domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	if !withFiles {
		return nil // 仅从记录中移除（保留文件）——当前实现与 withFiles 等价
	}
	if err := os.RemoveAll(l.Dir); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法删除设备目录（可能被运行中的模拟器占用）", err).
			WithHint("请先停止该设备的模拟器实例")
	}
	if platform.FileExists(l.IniPath) {
		if err := os.Remove(l.IniPath); err != nil {
			return domain.Wrap(domain.CodePermissionDenied, "无法删除 .ini 文件", err)
		}
	}
	return nil
}

// Clone 复制一个 AVD（跳过快照与运行时临时文件），并复制元数据。
//
// srcConfigOverride 用于把新名称写回 config.ini（AvdId / displayname）。
func (s *Store) Clone(srcName, dstName string) error {
	src := s.Resolve(srcName)
	dst := s.Resolve(dstName)
	if !src.Exists {
		return domain.Err(domain.CodeAvdNotFound, "源设备不存在: "+srcName)
	}
	if s.Exists(dstName) {
		return domain.Err(domain.CodeAvdExists, "已存在同名设备: "+dstName)
	}
	skip := func(rel string, isDir bool) bool {
		base := strings.ToLower(filepath.Base(rel))
		if isDir && (base == "snapshots" || base == "tmp" || base == "cache.img" || base == "userdata-qemu.img.qcow2") {
			return true
		}
		return false
	}
	if err := platform.CopyTree(src.Dir, dst.Dir, skip); err != nil {
		return domain.Wrap(domain.CodeUnknown, "复制设备目录失败", err)
	}

	// 修正 config.ini 中的身份字段
	config, err := ReadIni(dst.ConfigIni)
	if err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法读取新设备的 config.ini", err)
	}
	config["AvdId"] = dstName
	if dn, ok := config["avd.ini.displayname"]; ok {
		config["avd.ini.displayname"] = dn
	}
	if err := WriteIni(dst.ConfigIni, config); err != nil {
		return err
	}

	// 写 .ini 指向新目录
	ini := map[string]string{
		"avd.ini.encoding": "UTF-8",
		"path":             dst.Dir,
		"path.rel":         filepath.Join("avd", dstName+".avd"),
		"target":           config["target"],
	}
	if err := WriteIni(dst.IniPath, ini); err != nil {
		return err
	}

	// 元数据
	if meta, _ := s.ReadMeta(srcName); meta != nil {
		meta.Name = dstName
		meta.CreatedAt = platform.NowMs()
		meta.LastUsedAt = 0
		meta.Extra = map[string]string{"clonedFrom": srcName}
		_ = s.WriteMeta(dstName, meta)
	}
	return nil
}

// EnsureIni 在缺少 .ini 时按目录补写（修复被手工移动的 AVD）。
func (s *Store) EnsureIni(name string) error {
	l := s.Resolve(name)
	if platform.FileExists(l.IniPath) || !l.Exists {
		return nil
	}
	config, err := ReadIni(l.ConfigIni)
	if err != nil {
		return err
	}
	ini := map[string]string{
		"avd.ini.encoding": "UTF-8",
		"path":             l.Dir,
		"path.rel":         filepath.Join("avd", name+".avd"),
		"target":           config["target"],
	}
	return WriteIni(l.IniPath, ini)
}

// ---------------------------------------------------------------- 小工具

// SdkRootHint 已废弃：请使用 Store.SdkRoot 字段（见 SetSdkRoot）。

func metaDisplay(m *Meta) string {
	if m == nil {
		return ""
	}
	return m.DisplayName
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
	parts := strings.Split(tagIDs, ",")
	return strings.TrimSpace(parts[0])
}

// getCI 大小写不敏感地读取键（实测：写入方有时用 PlayStore.enabled，有时用 playstore.enabled）。
func getCI(values map[string]string, key string) string {
	if v, ok := values[key]; ok {
		return v
	}
	for k, v := range values {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func eqYes(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}

func atoi(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	neg := false
	if strings.HasPrefix(v, "-") {
		neg = true
		v = v[1:]
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}
