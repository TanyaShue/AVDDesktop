//go:build windows

package platform

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// windowsSystemProxy 读取 WinINET 的代理设置（HKCU\...\Internet Settings）。
func windowsSystemProxy() string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = key.Close() }()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	// 形式可能是 "host:port" 或 "http=host:port;https=host:port"
	if strings.Contains(server, "=") {
		for _, part := range strings.Split(server, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), "https") {
				return normalizeProxy(kv[1])
			}
		}
	}
	return normalizeProxy(server)
}
