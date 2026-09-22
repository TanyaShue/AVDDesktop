package sdk

import (
	"os"
	"path/filepath"
	"testing"

	"AVDDesktop/internal/platform"
)

func TestVerifyAndScanInstalledPackages(t *testing.T) {
	tools := platform.NewTools(filepath.Join(t.TempDir(), "sdk"))
	writePackageFixture(t, tools, "cmdline-tools;latest", tools.CmdlineTools, map[string]string{
		filepath.Join("bin", scriptName("sdkmanager")): "manager",
		filepath.Join("bin", scriptName("avdmanager")): "avdmanager",
	})
	writePackageFixture(t, tools, "platform-tools", tools.PlatformTools, map[string]string{
		"adb" + executableSuffix(): "adb",
		"package.xml":              "<repository/>",
	})
	writePackageFixture(t, tools, "emulator", tools.EmulatorDir, map[string]string{
		"emulator" + executableSuffix(): "emulator",
		"package.xml":                   "<repository/>",
	})
	imagePath := "system-images;android-35;google_apis;x86_64"
	imageDir, err := PackageDirectory(tools, imagePath)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFixture(t, tools, imagePath, imageDir, map[string]string{"package.xml": "<repository/>"})

	for _, pkgPath := range []string{"cmdline-tools;latest", "platform-tools", "emulator", imagePath} {
		if err := VerifyPackage(tools, pkgPath); err != nil {
			t.Fatalf("VerifyPackage(%s): %v", pkgPath, err)
		}
	}
	scanned := ScanInstalledPackages(tools)
	if len(scanned) != 4 {
		t.Fatalf("扫描包数量 = %d，期望 4: %+v", len(scanned), scanned)
	}
}

func TestVerifySystemImageRejectsInterruptedResidue(t *testing.T) {
	tools := platform.NewTools(filepath.Join(t.TempDir(), "sdk"))
	pkgPath := "system-images;android-35;google_apis;x86_64"
	dir, err := PackageDirectory(tools, pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte("Pkg.Path="+pkgPath+"\nPkg.Revision=9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPackage(tools, pkgPath); err == nil {
		t.Fatal("缺少 package.xml 的中断残留必须校验失败")
	}
}

func TestStagePackagesQuarantinesAndRestores(t *testing.T) {
	tools := platform.NewTools(filepath.Join(t.TempDir(), "sdk"))
	writePackageFixture(t, tools, "platform-tools", tools.PlatformTools, map[string]string{
		"adb" + executableSuffix(): "adb",
		"package.xml":              "<repository/>",
	})
	staged, err := stagePackages(tools, []string{"platform-tools"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || platform.DirExists(tools.PlatformTools) {
		t.Fatalf("强制修复时应暂存现有目录: %+v", staged)
	}
	if !platform.DirExists(tools.PlatformTools + ".repair-old") {
		t.Fatal("暂存目录不存在")
	}
	restoreStaged(staged)
	if !platform.DirExists(tools.PlatformTools) || platform.DirExists(tools.PlatformTools+".repair-old") {
		t.Fatal("恢复后目录状态不正确")
	}
}

func writePackageFixture(t *testing.T, _ platform.Tools, pkgPath, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	properties := "Pkg.Path=" + pkgPath + "\nPkg.Revision=1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func executableSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}
