package service

import (
	"context"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/sdk/localrepo"
	"AVDDesktop/internal/sdk/query"
	"AVDDesktop/internal/sdk/repo"
)

// repairSDKMetadata 检查并补写缺失的本地包元数据（<pkg>/package.xml）。
//
// 为什么需要：本项目自研安装器（internal/sdk/install）直接解压官方归档并整体替换包目录，
// 而 sdkmanager / avdmanager / Android Studio 判定"包已安装"的依据是包目录下的 package.xml。
// 缺该文件时会出现"文件明明在、官方工具却认为没装"的故障——最典型的是创建设备时报
//
//	Error: "emulator" package must be installed!
//
// （详见 docs/RESEARCH-NOTES.md §10）。任何失败都只记录，不阻塞调用方。
//
// 返回被补写的包路径。
func (rt *Runtime) repairSDKMetadata(ctx context.Context, logf func(level, scope, format string, args ...any)) []string {
	if logf == nil {
		logf = func(string, string, string, ...any) {}
	}
	comp := rt.Components()
	scanner := query.NewScanner(comp.Paths.SdkRoot)
	missing := scanner.MetadataMissing()
	if len(missing) == 0 {
		return nil
	}

	// 与安装/卸载互斥：抢不到锁说明正有写任务在进行（安装本身会补写元数据），跳过即可。
	unlock, ok := rt.locks.TryLock("sdk:" + comp.Paths.SdkRoot)
	if !ok {
		logf("warn", "sdk-metadata", "SDK 目录正被其它任务占用，跳过 %d 个组件的元数据补写", len(missing))
		return nil
	}
	defer unlock()

	var (
		fixed    []string
		deferred []string
	)
	// 第一轮：只用本地 source.properties 补写，无需网络：
	// 通用类型包（emulator / platform-tools / cmdline-tools / build-tools / cmake / ndk / extras）
	// 与系统镜像（type-details 由 AndroidVersion.* / SystemImage.* 重建）都能走这条路。
	for _, pkgPath := range missing {
		dir := filepath.Join(comp.Paths.SdkRoot, query.PackageDir(pkgPath))
		meta, err := localrepo.FromDir(pkgPath, dir)
		if err != nil {
			deferred = append(deferred, pkgPath)
			continue
		}
		if _, err := localrepo.Write(dir, meta); err != nil {
			logf("warn", "sdk-metadata", "补写 %s 失败：%v", pkgPath, err)
			continue
		}
		fixed = append(fixed, pkgPath)
	}

	// 第二轮：剩下的（主要是 platforms / sources）type-details 带专有字段，
	// 只有仓库索引能还原；拿不到就如实报告，让用户联网后重试。
	//
	// 注意：系统镜像不在主索引（repository2-3.xml）里，而在 sys-img/<tag>/sys-img2-3.xml，
	// 所以要按镜像的 tag 去拉对应索引（仅当第一轮失败、例如 source.properties 缺失时才走到）。
	if len(deferred) > 0 {
		source := rt.ActiveSource()
		mainIdx, mainErr := comp.Installer.ResolveIndex(ctx, source, false)
		sysIdx := map[string]*repo.Index{}
		sysErr := map[string]error{}
		resolveSysImg := func(tag string) (*repo.Index, error) {
			if idx, ok := sysIdx[tag]; ok {
				return idx, sysErr[tag]
			}
			idx, err := comp.Installer.ResolveSysImgIndex(ctx, source, tag, false)
			if err != nil {
				sysErr[tag] = err
				return nil, err
			}
			sysIdx[tag] = idx
			return idx, nil
		}

		for _, pkgPath := range deferred {
			pkg, found, src := repo.Package{}, false, (*repo.Index)(nil)
			if tag := sysImgTag(pkgPath); tag != "" {
				if idx, err := resolveSysImg(tag); err != nil {
					logf("warn", "sdk-metadata", "无法读取系统镜像索引（%s）：%v", tag, err)
				} else if p, ok := idx.Find(pkgPath); ok {
					pkg, found, src = p, true, idx
				}
			}
			if !found {
				if mainIdx == nil {
					if mainErr != nil {
						logf("warn", "sdk-metadata", "无法读取仓库索引，%s 暂未补写：%v", pkgPath, mainErr)
					}
					continue
				}
				if p, ok := mainIdx.Find(pkgPath); ok {
					pkg, found, src = p, true, mainIdx
				}
			}
			if !found {
				logf("warn", "sdk-metadata", "索引里没有 %s，无法补写元数据", pkgPath)
				continue
			}
			meta := localrepo.FromPackage(pkg)
			meta.ApplyIndex(src)
			dir := filepath.Join(comp.Paths.SdkRoot, query.PackageDir(pkgPath))
			if _, err := localrepo.Write(dir, meta); err != nil {
				logf("warn", "sdk-metadata", "补写 %s 失败：%v", pkgPath, err)
				continue
			}
			fixed = append(fixed, pkgPath)
		}
	}

	if len(fixed) == 0 {
		return nil
	}
	logf("info", "sdk-metadata", "已补写 %d 个组件的 SDK 元数据 package.xml：%s",
		len(fixed), strings.Join(fixed, "、"))
	rt.detector.Invalidate()
	rt.Emit("sdk:changed", map[string]any{"repaired": fixed})
	return fixed
}

// sysImgTag 从系统镜像包路径里取出 tag（system-images;<api>;<tag>;<abi>）。
// 非系统镜像返回空字符串。
func sysImgTag(pkgPath string) string {
	parts := strings.Split(pkgPath, ";")
	if len(parts) < 4 || parts[0] != "system-images" {
		return ""
	}
	return parts[2]
}
