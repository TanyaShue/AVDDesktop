// Package platform 封装宿主环境差异：路径解析、可写性/磁盘检查、环境变量注入、
// 原生对话框与单实例锁。
//
// 路径解析规则来自实测（见 docs/RESEARCH-NOTES.md §5.1）：
// AVD 主目录必须按 ANDROID_AVD_HOME → ANDROID_USER_HOME → ANDROID_SDK_HOME
// → ANDROID_PREFS_ROOT → %USERPROFILE%\.android 的顺序解析，否则会看不到用户的设备。
package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
)

// envOr 读取环境变量（忽略大小写不敏感平台的差异：Windows 已由 os 处理）。
func envOr(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// PathResolution 是一个路径及其解析来源。
type PathResolution struct {
	Path   string
	Source string
}

// ResolveAvdHome 按官方优先级解析 AVD 主目录。
//
// source 用于 UI 上解释"为什么是这个目录"。
func ResolveAvdHome(override string) PathResolution {
	if v := strings.TrimSpace(override); v != "" {
		return PathResolution{Path: filepath.Clean(v), Source: "设置"}
	}
	if v := envOr("ANDROID_AVD_HOME"); v != "" {
		return PathResolution{Path: filepath.Clean(v), Source: "环境变量 ANDROID_AVD_HOME"}
	}
	if v := envOr("ANDROID_USER_HOME"); v != "" {
		return PathResolution{Path: filepath.Join(v, "avd"), Source: "环境变量 ANDROID_USER_HOME/avd"}
	}
	if v := envOr("ANDROID_SDK_HOME"); v != "" {
		return PathResolution{Path: filepath.Join(v, ".android", "avd"), Source: "环境变量 ANDROID_SDK_HOME/.android/avd"}
	}
	if v := envOr("ANDROID_PREFS_ROOT"); v != "" {
		return PathResolution{Path: filepath.Join(v, ".android", "avd"), Source: "环境变量 ANDROID_PREFS_ROOT/.android/avd"}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return PathResolution{Path: filepath.Join(home, ".android", "avd"), Source: "默认 %USERPROFILE%\\.android\\avd"}
	}
	return PathResolution{Path: "", Source: "无法解析"}
}

// ResolveSdkRoot 返回当前应使用的 SDK 根目录（不含有效性校验）。
func ResolveSdkRoot(override string) PathResolution {
	if v := strings.TrimSpace(override); v != "" {
		return PathResolution{Path: filepath.Clean(v), Source: "设置"}
	}
	if v := envOr("ANDROID_HOME"); v != "" {
		return PathResolution{Path: filepath.Clean(v), Source: "环境变量 ANDROID_HOME"}
	}
	if v := envOr("ANDROID_SDK_ROOT"); v != "" {
		return PathResolution{Path: filepath.Clean(v), Source: "环境变量 ANDROID_SDK_ROOT"}
	}
	if c := DiscoverSdkRoots(""); len(c) > 0 {
		return PathResolution{Path: c[0].Path, Source: c[0].Source}
	}
	return PathResolution{Path: DefaultSdkRoot(), Source: "默认路径（尚未创建）"}
}

// DefaultSdkRoot 是推荐的新建 SDK 位置。
func DefaultSdkRoot() string {
	if runtime.GOOS == "windows" {
		if v := envOr("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "Android", "Sdk")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Android", "Sdk")
	}
	return filepath.Join(".", "android-sdk")
}

// DiscoverSdkRoots 枚举候选 SDK 根目录并按"有效性评分"降序返回。
func DiscoverSdkRoots(extra string) []domain.SdkRootCandidate {
	seen := map[string]bool{}
	var out []domain.SdkRootCandidate

	add := func(path, source string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, scoreCandidate(clean, source))
	}

	if v := strings.TrimSpace(extra); v != "" {
		add(v, "手动指定")
	}
	add(envOr("ANDROID_HOME"), "环境变量 ANDROID_HOME")
	add(envOr("ANDROID_SDK_ROOT"), "环境变量 ANDROID_SDK_ROOT")
	if v := envOr("ANDROID_SDK_HOME"); v != "" {
		add(v, "环境变量 ANDROID_SDK_HOME")
		add(filepath.Join(v, "Sdk"), "环境变量 ANDROID_SDK_HOME/Sdk")
	}
	if v := envOr("LOCALAPPDATA"); v != "" {
		add(filepath.Join(v, "Android", "Sdk"), "%LOCALAPPDATA%\\Android\\Sdk")
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "AppData", "Local", "Android", "Sdk"), "%USERPROFILE%\\AppData\\Local\\Android\\Sdk")
		add(filepath.Join(home, ".android", "sdk"), "%USERPROFILE%\\.android\\sdk")
		add(filepath.Join(home, "Android", "Sdk"), "%USERPROFILE%\\Android\\Sdk")
	}
	if v := envOr("ProgramFiles"); v != "" {
		add(filepath.Join(v, "Android", "Sdk"), "%ProgramFiles%\\Android\\Sdk")
		// Android Studio 自带 JDK 与可能的 sdk 目录
		add(filepath.Join(v, "Android", "Android Studio", "sdk"), "Android Studio 安装目录")
	}
	if v := envOr("ProgramFiles(x86)"); v != "" {
		add(filepath.Join(v, "Android", "android-sdk"), "%ProgramFiles(x86)%\\Android\\android-sdk")
		add(filepath.Join(v, "Android", "Android Studio", "sdk"), "Android Studio 安装目录 (x86)")
	}
	if runtime.GOOS == "windows" {
		add(`C:\Android\Sdk`, "常见路径 C:\\Android\\Sdk")
		add(`C:\Android\sdk`, "常见路径 C:\\Android\\sdk")
		add(`C:\SDK`, "常见路径 C:\\SDK")
	}
	// PATH 中的 adb / emulator 反推
	for _, dir := range searchPathDirs() {
		base := strings.ToLower(filepath.Base(dir))
		if base == "platform-tools" || base == "emulator" || base == "cmdline-tools" {
			add(filepath.Dir(dir), "从 PATH 推断")
		}
	}
	add(DefaultSdkRoot(), "推荐默认位置")

	// 只保留存在过的候选（推荐默认位置除外），再按分数排序
	filtered := make([]domain.SdkRootCandidate, 0, len(out))
	for _, c := range out {
		if c.Exists || c.Source == "推荐默认位置" || c.Source == "手动指定" {
			filtered = append(filtered, c)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Score > filtered[j].Score })
	return filtered
}

func searchPathDirs() []string {
	raw := os.Getenv("PATH")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func scoreCandidate(path, source string) domain.SdkRootCandidate {
	c := domain.SdkRootCandidate{Path: path, Source: source}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		c.Exists = true
		c.Score += 1
	}
	if hasDir(path, "cmdline-tools") {
		c.HasCmdlineTools = true
		c.Score += 4
	}
	if hasDir(path, "platform-tools") {
		c.HasPlatformTools = true
		c.Score += 2
	}
	if hasDir(path, "emulator") {
		c.HasEmulator = true
		c.Score += 2
	}
	if hasDir(path, "licenses") {
		c.Score += 1
	}
	if hasDir(path, "system-images") {
		c.Score += 1
	}
	if c.Score == 0 && !c.Exists {
		c.Score = 0
	}
	return c
}

func hasDir(base, name string) bool {
	st, err := os.Stat(filepath.Join(base, name))
	return err == nil && st.IsDir()
}

// InstallPaths 汇总某个 SDK 根目录下的关键绝对路径。
type InstallPaths struct {
	SdkRoot       string
	CmdlineTools  string // <sdk>/cmdline-tools/latest
	Sdkmanager    string
	Avdmanager    string
	PlatformTools string
	Adb           string
	Emulator      string
	EmulatorExe   string
	Licenses      string
	SystemImages  string
	Platforms     string
	BuildTools    string
	KnownPackages string
}

// NewInstallPaths 基于 SDK 根目录推导关键路径（不保证存在）。
func NewInstallPaths(sdkRoot string) InstallPaths {
	exe := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".exe"
		}
		return name
	}
	ct := filepath.Join(sdkRoot, "cmdline-tools", "latest")
	pt := filepath.Join(sdkRoot, "platform-tools")
	em := filepath.Join(sdkRoot, "emulator")
	return InstallPaths{
		SdkRoot:       sdkRoot,
		CmdlineTools:  ct,
		Sdkmanager:    filepath.Join(ct, "bin", "sdkmanager"+exeSuffix()),
		Avdmanager:    filepath.Join(ct, "bin", "avdmanager"+exeSuffix()),
		PlatformTools: pt,
		Adb:           filepath.Join(pt, exe("adb")),
		Emulator:      em,
		EmulatorExe:   filepath.Join(em, exe("emulator")),
		Licenses:      filepath.Join(sdkRoot, "licenses"),
		SystemImages:  filepath.Join(sdkRoot, "system-images"),
		Platforms:     filepath.Join(sdkRoot, "platforms"),
		BuildTools:    filepath.Join(sdkRoot, "build-tools"),
		KnownPackages: filepath.Join(sdkRoot, ".knownPackages"),
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".bat"
	}
	return ""
}

// FindJava 返回可用的 java 可执行文件路径（settings 覆盖 → JAVA_HOME → PATH → Android Studio 自带 JBR）。
func FindJava(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		if st, err := os.Stat(v); err == nil {
			if st.IsDir() {
				c := filepath.Join(v, "bin", javaName())
				if fileExists(c) {
					return c
				}
				return ""
			}
			return v
		}
	}
	if v := envOr("JAVA_HOME"); v != "" {
		c := filepath.Join(v, "bin", javaName())
		if fileExists(c) {
			return c
		}
	}
	for _, dir := range searchPathDirs() {
		c := filepath.Join(dir, javaName())
		if fileExists(c) {
			return c
		}
	}
	// Android Studio 自带的 JetBrains Runtime
	var candidates []string
	if v := envOr("ProgramFiles"); v != "" {
		candidates = append(candidates, filepath.Join(v, "Android", "Android Studio", "jbr"))
	}
	if v := envOr("ProgramFiles(x86)"); v != "" {
		candidates = append(candidates, filepath.Join(v, "Android", "Android Studio", "jbr"))
	}
	if v := envOr("LOCALAPPDATA"); v != "" {
		candidates = append(candidates, filepath.Join(v, "Programs", "Android Studio", "jbr"))
		candidates = append(candidates, filepath.Join(v, "JetBrains"))
	}
	for _, base := range candidates {
		c := filepath.Join(base, "bin", javaName())
		if fileExists(c) {
			return c
		}
	}
	return ""
}

func javaName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// ChildEnv 构造子进程环境变量，确保外部工具能找到 SDK 与 AVD 目录。
//
// 注意：绝不修改系统 PATH；只影响我们启动的子进程。
func ChildEnv(sdkRoot, avdHome string, inject bool) []string {
	env := os.Environ()
	if !inject {
		return env
	}
	set := func(key, value string) {
		if value == "" {
			return
		}
		prefix := key + "="
		for i, kv := range env {
			if strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(prefix)) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}
	set("ANDROID_HOME", sdkRoot)
	set("ANDROID_SDK_ROOT", sdkRoot)
	if avdHome != "" {
		// ANDROID_AVD_HOME 指向“包含 <name>.avd 与 <name>.ini 的目录”本身，
		// 而不是它的父目录（写错会让 avdmanager 把设备建到别处）。
		set("ANDROID_AVD_HOME", avdHome)
	}
	if sdkRoot != "" {
		paths := []string{
			filepath.Join(sdkRoot, "platform-tools"),
			filepath.Join(sdkRoot, "emulator"),
			filepath.Join(sdkRoot, "cmdline-tools", "latest", "bin"),
		}
		sep := string(os.PathListSeparator)
		set("PATH", strings.Join(paths, sep)+sep+os.Getenv("PATH"))
	}
	return env
}
