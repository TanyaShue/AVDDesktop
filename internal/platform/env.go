package platform

import "strings"

// WithEnv 返回 env 的副本，并把 key 设置为 value。
//
// 匹配 key 时不区分大小写，与 ChildEnv 的既有行为保持一致；已有 key 会原地替换，
// 缺失时追加，避免 exec.Cmd 的环境里出现同名变量的不确定覆盖顺序。
func WithEnv(env []string, key, value string) []string {
	out := make([]string, len(env))
	copy(out, env)
	prefix := key + "="
	for i, kv := range out {
		if strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(prefix)) {
			out[i] = prefix + value
			return out
		}
	}
	return append(out, prefix+value)
}
