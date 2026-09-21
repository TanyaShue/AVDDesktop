package sdk

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// Download 以单连接方式下载 url 到 dest，并校验 SHA-1（wantSHA1 为空时跳过校验）。
//
// 首次初始化只需要下载一个归档，因此这里不做分片、断点续传或限速。
// 代理走标准库的 ProxyFromEnvironment（HTTPS_PROXY / HTTP_PROXY）。
func Download(ctx context.Context, url, dest, wantSHA1 string, onProgress ProgressFunc) error {
	if strings.TrimSpace(url) == "" {
		return domain.Err(domain.CodeInvalidArgument, "下载地址为空")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建下载目录", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Wrap(domain.CodeInvalidArgument, "无法构造下载请求", err)
	}
	client := &http.Client{Timeout: 0} // 超时由 ctx 控制（下载大文件需要较长时间）
	resp, err := client.Do(req)
	if err != nil {
		return domain.Wrap(domain.CodeProcessFailed, "下载失败（请检查网络或代理设置）", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return domain.ErrDetail(domain.CodeProcessFailed,
			"下载失败：服务器返回 "+resp.Status, url)
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法写入下载文件", err)
	}

	hash := sha1.New()
	total := resp.ContentLength
	started := time.Now()
	lastReport := time.Now()
	var done int64

	buf := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return domain.Err(domain.CodeJobCanceled, "下载已取消")
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				_ = os.Remove(tmp)
				return domain.Wrap(domain.CodePermissionDenied, "写入下载文件失败", werr)
			}
			_, _ = hash.Write(buf[:n])
			done += int64(n)
			if onProgress != nil && time.Since(lastReport) > 200*time.Millisecond {
				lastReport = time.Now()
				onProgress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return domain.Wrap(domain.CodeProcessFailed, "下载中断", readErr)
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return domain.Wrap(domain.CodePermissionDenied, "下载文件写入失败", err)
	}
	if onProgress != nil {
		onProgress(done, total)
	}

	if wantSHA1 != "" {
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(got, wantSHA1) {
			_ = os.Remove(tmp)
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"下载文件校验失败（SHA-1 不匹配）", "期望 "+wantSHA1+"，实际 "+got)
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return domain.Wrap(domain.CodePermissionDenied, "无法保存下载文件", err)
	}
	_ = started
	return nil
}
