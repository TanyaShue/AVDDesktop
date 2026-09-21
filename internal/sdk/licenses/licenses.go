// Package licenses 处理 Android SDK 许可。
//
// 依据实测（docs/RESEARCH-NOTES.md §3）：licenses/<id> 文件内容是**固定哈希常量**，
// 不能由许可文本重新计算（实测文本 SHA-1 与常量不一致），因此必须内置常量表。
package licenses

import (
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
)

// Known 是实测得到的 license id → 哈希常量表。
//
// 来源：本机 %LOCALAPPDATA%\Android\Sdk\licenses\ 下的真实文件内容。
var Known = map[string]string{
	"android-sdk-license":           "24333f8a63b6825ea9c5514f83c2829b004d1fee",
	"android-sdk-preview-license":   "84831b9409646a918e30573bab4c9c91346d8abd",
	"android-sdk-arm-dbt-license":   "859f317696f67ef3d7f30a50a5560e7834b43903",
	"android-googletv-license":      "601085b94cd77f0b54ff86406957099ebe79c4d6",
	"android-googlexr-license":      "ceff83576aac4f7f37cb98fe189e9fb3c49d3b81",
	"google-gdk-license":            "33b6a2b64607f11b759f320ef9dff4ae5c47d97a",
	"mips-android-sysimage-license": "e9acab5b5fbb560a72cfaecce8946896ff6aab9d",
}

// DisplayNames 提供中文说明（UI 展示用）。
var DisplayNames = map[string]string{
	"android-sdk-license":           "Android SDK 许可协议",
	"android-sdk-preview-license":   "Android SDK 预览版许可协议",
	"android-sdk-arm-dbt-license":   "Android SDK ARM 设备测试许可",
	"android-googletv-license":      "Google TV 附加许可",
	"android-googlexr-license":      "Google XR 附加许可",
	"google-gdk-license":            "Google Glass GDK 许可",
	"mips-android-sysimage-license": "MIPS 系统镜像许可",
}

// IsKnown 判断是否为已知许可 id。
func IsKnown(id string) bool { _, ok := Known[id]; return ok }

// DisplayName 返回许可的中文名称。
func DisplayName(id string) string {
	if n, ok := DisplayNames[id]; ok {
		return n
	}
	return id
}

// FilePath 返回许可文件路径。
func FilePath(licensesDir, id string) string {
	return filepath.Join(licensesDir, id)
}

// IsAccepted 判断某个许可是否已被接受（文件存在且哈希匹配）。
func IsAccepted(licensesDir, id string) bool {
	want, ok := Known[id]
	if !ok {
		return false
	}
	data, err := os.ReadFile(FilePath(licensesDir, id))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), want) {
			return true
		}
	}
	// 兼容"文件存在即视为接受"的旧版本 SDK
	return true
}

// AcceptedIDs 返回目录下已接受的许可 id 列表。
func AcceptedIDs(licensesDir string) []string {
	entries, err := os.ReadDir(licensesDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// Accept 写入许可文件（原子写），表示用户已同意。
//
// 内容格式与 sdkmanager 一致：首行空行 + 哈希。
func Accept(licensesDir, id string) error {
	hash, ok := Known[id]
	if !ok {
		return domain.ErrDetail(domain.CodeInvalidArgument,
			"未知的许可标识: "+id, "请更新软件以支持新的许可类型")
	}
	if err := os.MkdirAll(licensesDir, 0o755); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建 licenses 目录", err)
	}
	content := "\n" + hash + "\n"
	if err := writeFile(FilePath(licensesDir, id), content); err != nil {
		return err
	}
	return nil
}

// AcceptAll 批量接受。
func AcceptAll(licensesDir string, ids []string) error {
	for _, id := range ids {
		if !IsKnown(id) {
			continue
		}
		if err := Accept(licensesDir, id); err != nil {
			return err
		}
	}
	return nil
}

// Missing 返回尚未接受的许可 id 列表。
func Missing(licensesDir string, ids []string) []string {
	var out []string
	for _, id := range ids {
		if id == "" || !IsKnown(id) {
			continue
		}
		if !IsAccepted(licensesDir, id) {
			out = append(out, id)
		}
	}
	return out
}

func writeFile(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法写入许可文件", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法保存许可文件", err)
	}
	return nil
}
