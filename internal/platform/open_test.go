package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveOpenTarget 锁定"打开位置"的目标解析规则。
//
// 真实问题：Wails 的 BrowserOpenURL 会拒绝 file:// 与含空格/反斜杠的路径，
// 且不返回错误，导致"打开目录"按钮点了没反应。修复后由本函数决定传给
// 文件管理器的路径，因此这几点必须被测试固定下来。
func TestResolveOpenTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sdk", "platform-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	file := filepath.Join(dir, "adb.exe")
	if err := os.WriteFile(file, []byte("stub"), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	t.Run("目录直接打开", func(t *testing.T) {
		got, selectFile, err := resolveOpenTarget(dir)
		if err != nil {
			t.Fatalf("解析目录失败: %v", err)
		}
		if got != dir || selectFile {
			t.Errorf("resolveOpenTarget(%q) = (%q, %v)，期望 (%q, false)", dir, got, selectFile, dir)
		}
	})

	t.Run("文件打开所在目录并选中", func(t *testing.T) {
		got, selectFile, err := resolveOpenTarget(file)
		if err != nil {
			t.Fatalf("解析文件失败: %v", err)
		}
		if got != file || !selectFile {
			t.Errorf("resolveOpenTarget(%q) = (%q, %v)，期望 (%q, true)", file, got, selectFile, file)
		}
	})

	t.Run("不存在的路径退到最近的已存在目录", func(t *testing.T) {
		// 例如组件未安装时的 <sdk>/cmdline-tools/latest/bin —— 不能报错，也不能凭空建目录。
		missing := filepath.Join(dir, "latest", "bin")
		got, selectFile, err := resolveOpenTarget(missing)
		if err != nil {
			t.Fatalf("解析不存在的路径失败: %v", err)
		}
		if got != dir || selectFile {
			t.Errorf("resolveOpenTarget(%q) = (%q, %v)，期望 (%q, false)", missing, got, selectFile, dir)
		}
		if DirExists(missing) {
			t.Errorf("解析过程不应创建目录: %s", missing)
		}
	})

	t.Run("相对路径转绝对路径", func(t *testing.T) {
		got, _, err := resolveOpenTarget(".")
		if err != nil {
			t.Fatalf("解析相对路径失败: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("期望绝对路径，得到 %q", got)
		}
	})

	t.Run("空路径报错", func(t *testing.T) {
		for _, in := range []string{"", "   ", "\t"} {
			if _, _, err := resolveOpenTarget(in); err == nil {
				t.Errorf("resolveOpenTarget(%q) 期望报错，实际成功", in)
			}
		}
	})
}

// TestOpenPathRejectsEmpty 保证空路径不会走到启动文件管理器那一步。
func TestOpenPathRejectsEmpty(t *testing.T) {
	if err := OpenPath("  "); err == nil {
		t.Error("OpenPath(空) 期望报错，实际成功")
	}
	if err := OpenTerminal(""); err == nil {
		t.Error("OpenTerminal(空) 期望报错，实际成功")
	}
}
