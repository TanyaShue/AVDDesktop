// Package jdk 负责把官方 Eclipse Temurin JDK 安装到软件自己的目录。
//
// 软件只使用 <Root>/jdk 下的 JDK，不读取系统 JAVA_HOME / PATH；缺失或损坏时
// 由环境准备流程重新下载安装。
package jdk

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/archive"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// Temurin 21 是当前 Android cmdline-tools 官方支持的 LTS JDK。
//
// 版本、文件名与 SHA-256 来自 Adoptium 官方 API / .sha256.txt：
// https://api.adoptium.net/v3/assets/latest/21/hotspot
const (
	temurinVersion = "21.0.12.1+1"
	temurinBaseURL = "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/"
	installTimeout = 30 * time.Minute
)

// Version 返回软件当前安装的 Temurin 版本。
func Version() string { return temurinVersion }

type artifact struct {
	Name   string
	URL    string
	SHA256 string
}

// artifactFor 返回指定平台对应的 Temurin 归档。
func artifactFor(goos, goarch string) (artifact, error) {
	var name string
	switch goos + "/" + goarch {
	case "windows/amd64":
		name = "OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.zip"
	case "windows/arm64":
		name = "OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.12.1_1.zip"
	case "darwin/amd64":
		name = "OpenJDK21U-jdk_x64_mac_hotspot_21.0.12.1_1.tar.gz"
	case "darwin/arm64":
		name = "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.12.1_1.tar.gz"
	case "linux/amd64":
		name = "OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz"
	case "linux/arm64":
		name = "OpenJDK21U-jdk_aarch64_linux_hotspot_21.0.12.1_1.tar.gz"
	default:
		return artifact{}, domain.ErrDetail(domain.CodeInvalidArgument,
			"当前平台暂不支持自动安装 JDK", goos+"/"+goarch)
	}

	sums := map[string]string{
		"OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.zip":      "f9d6e191ab098c0d416e7d588a24420a8621cd2f4720dab2459b8b7b2d2d8b4e",
		"OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.12.1_1.zip":  "ccf2e51f527d542a70ba5794a600d3aac04b4e967950e227834c7566cb1bec7b",
		"OpenJDK21U-jdk_x64_mac_hotspot_21.0.12.1_1.tar.gz":       "44db0f08196daf19a47f90d13388b0c943b67663cb537f998fe29e836fa842ce",
		"OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.12.1_1.tar.gz":   "3623232f33a9c3baadf304480b2535f9a3cba8a58d42ecbb438ba267315d9998",
		"OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz":     "ce79869e1307ed8ee1e2baa86a412b1eb5b75d10a01006d788a6f968bcfaee94",
		"OpenJDK21U-jdk_aarch64_linux_hotspot_21.0.12.1_1.tar.gz": "23e37e026f12f3e706f18938ff611db3032d075b09d0879a25d06718c773e223",
	}
	sum, ok := sums[name]
	if !ok || sum == "" {
		return artifact{}, domain.ErrDetail(domain.CodeInvalidArgument, "JDK 归档缺少校验值", name)
	}
	return artifact{Name: name, URL: temurinBaseURL + name, SHA256: sum}, nil
}

// Install 从 Adoptium 官方源下载并安装 Temurin JDK。
func Install(ctx context.Context, root, home, cacheDir string, onProgress func(done, total int64)) error {
	return installFromSource(ctx, ResolveSource(OfficialSourceID), root, home, cacheDir, onProgress)
}

// InstallFromSource 从指定 JDK 镜像下载并安装 Temurin JDK。
//
// 下载完成后始终使用程序内置的 SHA-256 校验，镜像站无法篡改归档内容。
// 安装先落到 root.staging，校验 java 可执行文件存在后再整体替换 root，避免留下
// 半成品；旧 JDK 会临时挪到 root.old，替换失败时回滚。
func InstallFromSource(ctx context.Context, sourceID, root, home, cacheDir string, onProgress func(done, total int64)) error {
	source, ok := FindSource(sourceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "JDK 镜像源不存在: "+strings.TrimSpace(sourceID))
	}
	return installFromSource(ctx, source, root, home, cacheDir, onProgress)
}

func installFromSource(ctx context.Context, source domain.MirrorSource, root, home, cacheDir string, onProgress func(done, total int64)) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(home) == "" {
		return domain.Err(domain.CodeInvalidArgument, "JDK 安装目录为空")
	}
	spec, err := artifactForSource(source, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if err := platform.EnsureDir(filepath.Dir(root)); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建软件目录", err)
	}
	if err := platform.EnsureDir(cacheDir); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建缓存目录", err)
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	archivePath := filepath.Join(cacheDir, spec.Name)
	if err := archive.Download(ctx, spec.URL, archivePath, spec.SHA256, onProgress); err != nil {
		return err
	}
	if err := installArchive(ctx, root, home, archivePath); err != nil {
		return err
	}
	_ = os.Remove(archivePath)
	return nil
}

// installArchive 从已经校验过的归档安装 JDK；独立出来便于用本地测试归档做单测。
func installArchive(ctx context.Context, root, home, archivePath string) error {
	rel, err := filepath.Rel(root, home)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return domain.ErrDetail(domain.CodeInvalidArgument,
			"JDK Home 必须位于软件 JDK 目录内", home)
	}

	staging := root + ".staging"
	old := root + ".old"
	_ = os.RemoveAll(staging)
	_ = os.RemoveAll(old)

	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		err = archive.ExtractZip(ctx, archivePath, staging)
	} else {
		err = archive.ExtractTarGz(ctx, archivePath, staging)
	}
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}

	stagedJava := filepath.Join(staging, rel, "bin", javaExeName())
	if !platform.FileExists(stagedJava) {
		_ = os.RemoveAll(staging)
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"JDK 归档内容不符合预期", "缺少 "+filepath.Join(rel, "bin", javaExeName()))
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(stagedJava, 0o755)
	}

	hadOld := platform.DirExists(root)
	if hadOld {
		if err := os.Rename(root, old); err != nil {
			_ = os.RemoveAll(staging)
			return domain.Wrap(domain.CodePermissionDenied, "无法替换已有 JDK 目录", err)
		}
	} else if info, err := os.Lstat(root); err == nil && !info.IsDir() {
		// 极端情况下同名路径是文件：移除后再启用新目录。
		if err := os.Remove(root); err != nil {
			_ = os.RemoveAll(staging)
			return domain.Wrap(domain.CodePermissionDenied, "无法替换 JDK 路径", err)
		}
	}
	if err := os.Rename(staging, root); err != nil {
		if hadOld {
			_ = os.Rename(old, root)
		}
		_ = os.RemoveAll(staging)
		return domain.Wrap(domain.CodePermissionDenied, "无法启用 JDK", err)
	}
	_ = os.RemoveAll(old)
	return nil
}

func javaExeName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}
