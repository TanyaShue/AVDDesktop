package platform

import (
	"os"
	"strings"
)

// SystemProxy 返回系统代理地址（用于 domain.ProxySystem 模式）。
//
// 优先读取环境变量（HTTPS_PROXY/HTTP_PROXY，大小写皆可）；
// Windows 下若未设置环境变量，尝试读取注册表 WinINET 代理设置。
func SystemProxy() string {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return normalizeProxy(v)
		}
	}
	if v := windowsSystemProxy(); v != "" {
		return v
	}
	return ""
}

func normalizeProxy(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "://") {
		v = "http://" + v
	}
	return v
}
