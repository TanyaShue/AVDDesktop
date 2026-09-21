package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/mirror"
	"AVDDesktop/internal/proc"
)

// itoa 是 strconv.Itoa 的短别名，减少模板代码噪音。
func itoa(v int) string { return strconv.Itoa(v) }

// atoi 忽略非法字符的宽松整数解析。
func atoi(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}

// nonEmpty 返回第一个非空字符串。
func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// path_Join 是 filepath.Join 的别名（避免与 service 包内其它标识符混淆）。
func path_Join(parts ...string) string { return filepath.Join(parts...) }

// jsonMarshalIndent 缩进序列化（诊断包里的 JSON 需要人可读）。
func jsonMarshalIndent(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, domain.Wrap(domain.CodeUnknown, "无法序列化诊断数据", err)
	}
	return append(data, '\n'), nil
}

// outputCommand 执行外部命令并返回合并输出。
func outputCommand(ctx context.Context, name string, args []string, env []string) (string, error) {
	return proc.Output(ctx, name, args, proc.Options{Env: env, Timeout: 60 * time.Second})
}

// removeAll 删除目录树（Windows 下文件被占用会失败，由调用方转成可读错误）。
func removeAll(path string) error { return os.RemoveAll(path) }

// mirrorSources 返回当前生效的镜像源列表（内置 + 自定义）。
func mirrorSources(rt *Runtime) []domain.MirrorSource {
	return mirror.Merge(rt.settings.Get().CustomSources)
}

// findSource 在内置 + 自定义源中查找。
func findSource(sources []domain.MirrorSource, id string) (domain.MirrorSource, bool) {
	return mirror.Find(sources, id)
}
