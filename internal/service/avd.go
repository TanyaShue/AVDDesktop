package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"AVDDesktop/internal/avd/backend"
	"AVDDesktop/internal/avd/profile"
	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
)

// AvdService 提供 AVD 的增删改查。
type AvdService struct{ rt *Runtime }

// NewAvdService 创建 AvdService。
func NewAvdService(rt *Runtime) *AvdService { return &AvdService{rt: rt} }

// List 返回所有设备摘要（合并运行状态与占用空间）。
func (s *AvdService) List() ([]domain.AvdSummary, error) {
	comp := s.rt.Components()
	items, err := comp.Store.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if inst, ok := comp.Launcher.ByAvd(items[i].Name); ok {
			items[i].State = inst.State
			items[i].InstanceID = inst.ID
			items[i].Serial = inst.Serial
			items[i].Port = inst.Port
		} else if items[i].State == "" {
			items[i].State = domain.AvdStopped
		}
	}
	// 把快照中已停止但仍在列表里的实例状态补齐
	return items, nil
}

// Get 返回设备详情。
func (s *AvdService) Get(name string) (*domain.AvdDetail, error) {
	comp := s.rt.Components()
	detail, err := comp.Store.Detail(name)
	if err != nil {
		return nil, err
	}
	if inst, ok := comp.Launcher.ByAvd(name); ok {
		detail.Summary.State = inst.State
		detail.Summary.InstanceID = inst.ID
		detail.Summary.Serial = inst.Serial
		detail.Summary.Port = inst.Port
	}
	return &detail, nil
}

// ListProfiles 返回设备档案（优先 avdmanager，失败用内置兜底）。
func (s *AvdService) ListProfiles(refresh bool) ([]domain.DeviceProfile, error) {
	comp := s.rt.Components()
	profiles, err := profile.List(s.rt.Context(), comp.Paths.Avdmanager, comp.Env)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Category != profiles[j].Category {
			return categoryOrder(profiles[i].Category) < categoryOrder(profiles[j].Category)
		}
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, nil
}

// ListConfigSchema 返回 config.ini 配置项 schema（UI 动态渲染表单）。
func (s *AvdService) ListConfigSchema() []domain.HwConfigItem {
	return store.Schema
}

// ConfigSchemaMetadata 返回分组顺序与标签。
func (s *AvdService) ConfigSchemaMetadata() map[string]any {
	return map[string]any{
		"groupOrder":  store.GroupOrder,
		"groupLabels": store.GroupLabels,
		"presets":     store.ScreenPresets,
	}
}

// ValidateName 校验名称并给出建议。
func (s *AvdService) ValidateName(name string) (*domain.NameValidation, error) {
	comp := s.rt.Components()
	res := comp.Store.ValidateName(name)
	return &res, nil
}

// Create 创建 AVD，返回 jobID。
func (s *AvdService) Create(spec domain.AvdSpec) (string, error) {
	comp := s.rt.Components()
	if v := comp.Store.ValidateName(spec.Name); !v.Valid {
		return "", domain.Err(domain.CodeAvdNameInvalid, v.Reason)
	}
	if _, _, _, err := backend.SplitSystemImage(spec.SystemImagePath); err != nil {
		return "", err
	}
	relDir, err := backend.SystemImageDir(spec.SystemImagePath)
	if err != nil {
		return "", err
	}
	imgDir := filepath.Join(comp.Paths.SdkRoot, relDir)
	if !platform.DirExists(imgDir) {
		return "", domain.ErrDetail(domain.CodeImageNotInstalled,
			"系统镜像尚未安装", spec.SystemImagePath+"\n期望目录: "+imgDir).
			WithAction("installImage", "下载该镜像", spec.SystemImagePath)
	}

	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有创建/删除设备的任务正在进行")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobAvdCreate,
		Title:    "创建设备 " + spec.Name,
		Subtitle: spec.SystemImagePath,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		be := backend.Select(spec, backend.Deps{
			Paths: comp.Paths,
			Store: comp.Store,
			Env:   comp.Env,
			OnOutput: func(stream, line string) {
				level := "info"
				if stream == "stderr" {
					level = "warn"
				}
				j.Log(level, "avdmanager", line)
			},
		})
		j.SetPhase("正在创建（后端：" + be.Kind() + "）")
		res, err := be.Create(ctx, spec, backend.Deps{
			Paths: comp.Paths,
			Store: comp.Store,
			Env:   comp.Env,
			OnOutput: func(stream, line string) {
				j.Log("info", be.Kind(), line)
			},
		})
		if err != nil {
			return err
		}
		for _, w := range res.Warnings {
			j.Logf("warn", "create", "%s", w)
		}
		j.Logf("info", "create", "设备 %s 创建完成（%s）", spec.Name, res.Backend)
		s.rt.Emit("avd:changed", map[string]any{"action": "created", "name": spec.Name})
		return nil
	})
	return j.ID(), nil
}

// Update 修改已有 AVD 的配置。
func (s *AvdService) Update(name string, patch domain.AvdPatch) error {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(name)
	if !layout.Exists {
		return domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	if inst, ok := comp.Launcher.ByAvd(name); ok && inst.State != domain.AvdStopped && inst.State != domain.AvdError {
		return domain.Err(domain.CodeFileInUse, "设备正在运行，修改配置需要先停止它").
			WithHint("运行中的修改不会生效，请先停止设备")
	}
	config, err := store.ReadIni(layout.ConfigIni)
	if err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	store.ApplyHW(config, patch.HW)
	for _, k := range patch.RemoveHW {
		delete(config, k)
	}
	if patch.DisplayName != nil && strings.TrimSpace(*patch.DisplayName) != "" {
		config["avd.ini.displayname"] = strings.TrimSpace(*patch.DisplayName)
	}
	if err := store.WriteIni(layout.ConfigIni, config); err != nil {
		return err
	}

	meta, _ := comp.Store.ReadMeta(name)
	if meta == nil {
		meta = &store.Meta{Name: name, CreatedAt: platform.NowMs()}
	}
	if patch.DisplayName != nil {
		meta.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.Tags != nil {
		meta.Tags = patch.Tags
	}
	if patch.Note != nil {
		meta.Note = *patch.Note
	}
	if patch.Launch != nil {
		meta.LaunchPreset = patch.Launch
	}
	meta.UpdatedAt = platform.NowMs()
	if err := comp.Store.WriteMeta(name, meta); err != nil {
		return err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "updated", "name": name})
	return nil
}

// CloneRequest 是克隆请求。
type CloneRequest struct {
	SourceName string `json:"sourceName"`
	NewName    string `json:"newName"`
}

// Clone 克隆设备。
func (s *AvdService) Clone(req CloneRequest) (string, error) {
	comp := s.rt.Components()
	if v := comp.Store.ValidateName(req.NewName); !v.Valid {
		return "", domain.Err(domain.CodeAvdNameInvalid, v.Reason)
	}
	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有创建/删除设备的任务正在进行")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobAvdClone,
		Title: "克隆设备 " + req.SourceName + " → " + req.NewName,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		j.SetPhase("正在复制文件")
		if err := comp.Store.Clone(req.SourceName, req.NewName); err != nil {
			return err
		}
		j.Logf("info", "clone", "克隆完成：%s", req.NewName)
		s.rt.Emit("avd:changed", map[string]any{"action": "created", "name": req.NewName})
		return nil
	})
	return j.ID(), nil
}

// Rename 重命名设备（同时更新 .ini / 目录 / 元数据）。
func (s *AvdService) Rename(oldName, newName string) error {
	comp := s.rt.Components()
	if oldName == newName {
		return nil
	}
	if v := comp.Store.ValidateName(newName); !v.Valid {
		return domain.Err(domain.CodeAvdNameInvalid, v.Reason)
	}
	if _, ok := comp.Launcher.ByAvd(oldName); ok {
		return domain.Err(domain.CodeFileInUse, "设备正在运行，无法重命名")
	}
	old := comp.Store.Resolve(oldName)
	if !old.Exists {
		return domain.Err(domain.CodeAvdNotFound, "设备不存在: "+oldName)
	}
	newLayout := comp.Store.Resolve(newName)

	if err := os.Rename(old.Dir, newLayout.Dir); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法重命名设备目录", err)
	}
	config, err := store.ReadIni(newLayout.ConfigIni)
	if err != nil {
		// 回滚目录名
		_ = os.Rename(newLayout.Dir, old.Dir)
		return domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	config["AvdId"] = newName
	if err := store.WriteIni(newLayout.ConfigIni, config); err != nil {
		return err
	}
	ini := map[string]string{
		"avd.ini.encoding": "UTF-8",
		"path":             newLayout.Dir,
		"path.rel":         filepath.Join("avd", filepath.Base(newLayout.Dir)),
		"target":           config["target"],
	}
	if err := store.WriteIni(newLayout.IniPath, ini); err != nil {
		return err
	}
	_ = os.Remove(old.IniPath)
	if meta, _ := comp.Store.ReadMeta(newName); meta != nil {
		meta.Name = newName
		_ = comp.Store.WriteMeta(newName, meta)
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "renamed", "name": newName, "oldName": oldName})
	return nil
}

// DeleteRequest 是删除请求。
type DeleteRequest struct {
	Name        string `json:"name"`
	DeleteFiles bool   `json:"deleteFiles"`
}

// Delete 删除设备，返回 jobID。
func (s *AvdService) Delete(req DeleteRequest) (string, error) {
	comp := s.rt.Components()
	if inst, ok := comp.Launcher.ByAvd(req.Name); ok && inst.State != domain.AvdStopped && inst.State != domain.AvdError {
		if err := comp.Launcher.Stop(s.rt.Context(), inst.ID, false); err != nil {
			return "", err
		}
	}
	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有创建/删除设备的任务正在进行")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobAvdDelete,
		Title: "删除设备 " + req.Name,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		j.SetPhase("正在删除文件")
		if err := comp.Store.Delete(req.Name, req.DeleteFiles); err != nil {
			return err
		}
		j.Logf("info", "delete", "设备 %s 已删除", req.Name)
		s.rt.Emit("avd:changed", map[string]any{"action": "deleted", "name": req.Name})
		return nil
	})
	return j.ID(), nil
}

// WipeData 清除用户数据（删除 userdata-qemu.img 等运行期文件）。
func (s *AvdService) WipeData(name string) (string, error) {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(name)
	if !layout.Exists {
		return "", domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	if _, ok := comp.Launcher.ByAvd(name); ok {
		return "", domain.Err(domain.CodeFileInUse, "设备正在运行，请先停止后再清除数据")
	}
	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有设备写任务正在进行")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobSnapshot,
		Title: "清除数据 " + name,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		targets := []string{
			"userdata-qemu.img", "userdata-qemu.img.qcow2", "cache.img",
			"userdata.img.qcow2", "snapshots", "sdcard.img",
		}
		for _, t := range targets {
			p := filepath.Join(layout.Dir, t)
			if !platform.DirExists(p) && !platform.FileExists(p) {
				continue
			}
			j.SetPhase("删除 " + t)
			if err := os.RemoveAll(p); err != nil {
				return domain.Wrap(domain.CodePermissionDenied, "无法删除 "+t, err)
			}
		}
		s.rt.Emit("avd:changed", map[string]any{"action": "wiped", "name": name})
		return nil
	})
	return j.ID(), nil
}

// ReadConfigRaw 返回 config.ini 原文（供"直编"模式）。
func (s *AvdService) ReadConfigRaw(name string) (string, error) {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(name)
	if !layout.Exists {
		return "", domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	data, err := os.ReadFile(layout.ConfigIni)
	if err != nil {
		return "", domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	return string(data), nil
}

// WriteConfigRawRequest 是直编保存请求。
type WriteConfigRawRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	DryRun  bool   `json:"dryRun"`
}

// WriteConfigRaw 保存 config.ini 原文并返回差异。
func (s *AvdService) WriteConfigRaw(req WriteConfigRawRequest) (*domain.ConfigDiff, error) {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(req.Name)
	if !layout.Exists {
		return nil, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+req.Name)
	}
	before, err := store.ReadIni(layout.ConfigIni)
	if err != nil {
		return nil, domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	after := store.ParseIni(req.Content)
	diff := store.DiffConfig(before, after)
	if req.DryRun {
		return &diff, nil
	}
	if len(after) == 0 {
		return nil, domain.Err(domain.CodeInvalidArgument, "解析后的配置为空，已拒绝保存")
	}
	if _, err := os.Stat(layout.ConfigIni); err == nil {
		_, _ = platform.CopyFile(layout.ConfigIni, layout.ConfigIni+".bak")
	}
	if err := platform.WriteFileAtomic(layout.ConfigIni, []byte(store.FormatIni(after)), 0o644); err != nil {
		return nil, err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "updated", "name": req.Name})
	return &diff, nil
}

// OpenFolder 在文件管理器中打开 AVD 目录。
func (s *AvdService) OpenFolder(name string) error {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(name)
	env := NewEnvService(s.rt)
	return env.OpenInExplorer(layout.Dir)
}

// ComputeCommand 生成等效命令行（供 UI 展示）。
func (s *AvdService) ComputeCommand(spec domain.AvdSpec) map[string]string {
	avdArgs := []string{"create", "avd", "-n", spec.Name, "-k", spec.SystemImagePath}
	if spec.ProfileID != "" {
		avdArgs = append(avdArgs, "-d", spec.ProfileID)
	}
	if spec.SDCardSize != "" {
		avdArgs = append(avdArgs, "-c", spec.SDCardSize)
	}
	avdArgs = append(avdArgs, "-f")

	comp := s.rt.Components()
	emulatorArgs := []string{"-avd", spec.Name}
	if spec.LaunchDefaults != nil {
		emulatorArgs = launchArgsFor(spec.Name, *spec.LaunchDefaults)
	}
	return map[string]string{
		"avdmanager": comp.Paths.Avdmanager + " " + strings.Join(avdArgs, " "),
		"emulator":   comp.Paths.EmulatorExe + " " + strings.Join(emulatorArgs, " "),
	}
}

// ExportRequest 是导出请求（打包 .avd 目录与 .ini）。
type ExportRequest struct {
	Name             string `json:"name"`
	TargetZip        string `json:"targetZip"`
	IncludeSnapshots bool   `json:"includeSnapshots"`
}

// Export 把设备导出为一个 zip 包（内部结构：<name>.ini + <name>.avd/...）。
func (s *AvdService) Export(req ExportRequest) (string, error) {
	comp := s.rt.Components()
	layout := comp.Store.Resolve(req.Name)
	if !layout.Exists {
		return "", domain.Err(domain.CodeAvdNotFound, "设备不存在: "+req.Name)
	}
	if strings.TrimSpace(req.TargetZip) == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "未指定导出文件路径")
	}
	if _, ok := comp.Launcher.ByAvd(req.Name); ok {
		return "", domain.Err(domain.CodeFileInUse,
			"设备正在运行，无法导出（数据文件正在被写入）").
			WithHint("请先停止设备后重试")
	}

	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有设备写任务正在进行")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobExport,
		Title: "导出设备 " + req.Name,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		j.SetPhase("正在打包")
		written, err := comp.Store.Export(ctx, req.Name, req.TargetZip, req.IncludeSnapshots, func(done, total int64, name string, line string) {
			if line != "" {
				j.Log("info", "export", line)
			}
			if total > 0 {
				j.SetBytes(done, total, 0)
			}
		})
		if err != nil {
			return err
		}
		j.Logf("info", "export", "已导出到 %s（%s）", req.TargetZip, platform.HumanSize(written))
		return nil
	})
	return j.ID(), nil
}

// ImportRequest 是导入请求。
type ImportRequest struct {
	ZipPath string `json:"zipPath"`
	// Name 为空时使用包内的设备名；重名时自动加后缀。
	Name string `json:"name"`
}

// Import 从 zip 包导入设备（恢复到当前 AVD 目录并修正配置中的路径）。
func (s *AvdService) Import(req ImportRequest) (string, error) {
	comp := s.rt.Components()
	if !platform.FileExists(req.ZipPath) {
		return "", domain.Err(domain.CodePathNotFound, "文件不存在: "+req.ZipPath)
	}
	unlock, ok := s.rt.locks.TryLock("avd:" + comp.Store.AvdHome)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有设备写任务正在进行")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobAvdCreate,
		Title: "导入设备 " + filepath.Base(req.ZipPath),
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		j.SetPhase("正在解包")
		name, err := comp.Store.Import(ctx, req.ZipPath, req.Name, func(done, total int64, current string) error {
			if err := ctx.Err(); err != nil {
				return domain.Err(domain.CodeJobCanceled, "操作已取消")
			}
			if total > 0 {
				j.SetBytes(done, total, 0)
			}
			return nil
		})
		if err != nil {
			return err
		}
		j.Logf("info", "import", "已导入设备 %s", name)
		s.rt.Emit("avd:changed", map[string]any{"action": "created", "name": name})
		return nil
	})
	return j.ID(), nil
}

// SnapshotDir 返回快照目录（供 UI 显示占用）。
func (s *AvdService) SnapshotDir(name string) string {
	comp := s.rt.Components()
	return filepath.Join(comp.Store.Resolve(name).Dir, "snapshots")
}

func categoryOrder(category string) int {
	switch category {
	case "phone":
		return 0
	case "tablet":
		return 1
	case "desktop":
		return 2
	case "tv":
		return 3
	case "automotive":
		return 4
	case "wear":
		return 5
	case "xr":
		return 6
	default:
		return 7
	}
}

// 与 launch 包解耦的启动参数构造（避免 service → launch 的参数重复实现）。
func launchArgsFor(name string, opts domain.LaunchOptions) []string {
	out := []string{"-avd", name}
	if opts.ColdBoot {
		out = append(out, "-no-snapshot-load")
	}
	if opts.NoWindow {
		out = append(out, "-no-window")
	}
	if opts.GPUMode != "" && opts.GPUMode != "auto" {
		out = append(out, "-gpu", opts.GPUMode)
	}
	return out
}

var _ = time.Second
