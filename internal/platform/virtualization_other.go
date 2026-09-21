//go:build !windows

package platform

import "context"

// VirtualizationInfo 在非 Windows 平台上的占位定义（macOS 用 HVF，Linux 用 KVM）。
type VirtualizationInfo struct {
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
	Source    string `json:"source,omitempty"`
	CPU       string `json:"cpu,omitempty"`
	Caption   string `json:"caption,omitempty"`
	Version   string `json:"version,omitempty"`
	Build     string `json:"build,omitempty"`
}

// VirtualizationInfoCached 在非 Windows 平台不做探测（accelerate-check 已足够）。
func VirtualizationInfoCached(ctx context.Context) VirtualizationInfo {
	return VirtualizationInfo{Available: false, Error: "仅在 Windows 上探测虚拟化能力"}
}

// LongPathsEnabled 在类 Unix 平台无此限制。
func LongPathsEnabled() bool { return true }

// WindowsBuild 在非 Windows 平台返回空串。
func WindowsBuild() string { return "" }
