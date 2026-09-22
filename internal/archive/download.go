// Package archive 提供下载、校验与归档解压能力，供 SDK / JDK 自举共用。
package archive

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// Download 以单连接方式下载 url 到 dest，并校验 SHA-1 或 SHA-256（wantSHA 为空时跳过校验）。
//
// 校验值长度决定算法：40 位为 SHA-1，64 位为 SHA-256。首次初始化只需要
// 下载一个归档，因此这里不做分片、断点续传或限速。代理走标准库的
// ProxyFromEnvironment（HTTPS_PROXY / HTTP_PROXY）。
func Download(ctx context.Context, url, dest, wantSHA string, onProgress func(done, total int64)) error {
	if strings.TrimSpace(url) == "" {
		return domain.Err(domain.CodeInvalidArgument, "下载地址为空")
	}
	wantSHA = strings.TrimSpace(wantSHA)
	var hasher hash.Hash
	switch len(wantSHA) {
	case 0:
		// 未提供校验值：仅用于测试或调用方明确接受风险时。
	case 40:
		hasher = sha1.New()
	case 64:
		hasher = sha256.New()
	default:
		return domain.ErrDetail(domain.CodeInvalidArgument,
			"不支持的校验值长度", "期望 40 位 SHA-1 或 64 位 SHA-256，实际 "+wantSHA)
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

	total := resp.ContentLength
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
			if hasher != nil {
				_, _ = hasher.Write(buf[:n])
			}
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

	if hasher != nil {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, wantSHA) {
			_ = os.Remove(tmp)
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"下载文件校验失败", "期望 "+wantSHA+"，实际 "+got)
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return domain.Wrap(domain.CodePermissionDenied, "无法保存下载文件", err)
	}
	return nil
}
