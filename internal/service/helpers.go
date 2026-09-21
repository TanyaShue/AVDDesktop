package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/proc"
)

// itoa 是 strconv.Itoa 的短别名。
func itoa(v int) string { return strconv.Itoa(v) }

// nonEmpty 返回第一个非空字符串。
func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// path_Join 是 filepath.Join 的别名。
func path_Join(parts ...string) string { return filepath.Join(parts...) }

// jsonMarshalIndent 缩进序列化。
func jsonMarshalIndent(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, domain.Wrap(domain.CodeUnknown, "无法序列化数据", err)
	}
	return append(data, '\n'), nil
}

// outputCommand 执行外部命令并返回输出（30 秒超时）。
func outputCommand(ctx context.Context, name string, args []string, env []string) (string, error) {
	return proc.Output(ctx, name, args, proc.Options{Env: env, Timeout: 30 * time.Second})
}
