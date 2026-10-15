package main

import (
	"runtime"
	"strings"
	"testing"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/domain"
)

// TestQuitSelectionConfirmed 覆盖 issue #3：Wails 在 Windows / Linux 的
// QuestionDialog 上返回原生 "Yes" / "No"（自定义 Buttons 被忽略），
// macOS 才返回自定义按钮文案。确认结果必须被正确识别，否则窗口无法关闭、
// 模拟器也不会停止。
func TestQuitSelectionConfirmed(t *testing.T) {
	cases := []struct {
		name      string
		selection string
		want      bool
	}{
		{"Windows/Linux 确认（原生 Yes）", "Yes", true},
		{"Windows/Linux 取消（原生 No）", "No", false},
		{"小写 yes", "yes", true},
		{"带首尾空白的 Yes", "  Yes\n", true},
		{"macOS 确认（自定义文案）", quitButtonConfirm, true},
		{"macOS 取消（自定义文案）", quitButtonCancel, false},
		{"Linux Esc / 关闭窗口返回空串", "", false},
		{"Windows 其他返回（Cancel）", "Cancel", false},
		{"未知文案按取消处理", "Whatever", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quitSelectionConfirmed(tc.selection); got != tc.want {
				t.Fatalf("quitSelectionConfirmed(%q) = %v，期望 %v", tc.selection, got, tc.want)
			}
		})
	}
}

// TestQuitConfirmDialogOptions 锁定确认框参数，并记录 issue #3 的成因：
// 对话框只声明了自定义按钮文案，而 Windows / Linux 的原生实现并不使用它们。
func TestQuitConfirmDialogOptions(t *testing.T) {
	const active = 2
	opts := quitConfirmDialogOptions(active)

	if opts.Type != wailsruntime.QuestionDialog {
		t.Fatalf("对话框类型 = %v，期望 QuestionDialog", opts.Type)
	}
	if len(opts.Buttons) != 2 || opts.Buttons[0] != quitButtonConfirm || opts.Buttons[1] != quitButtonCancel {
		t.Fatalf("Buttons = %q，期望 [%q %q]", opts.Buttons, quitButtonConfirm, quitButtonCancel)
	}
	for _, b := range opts.Buttons {
		if strings.EqualFold(strings.TrimSpace(b), "yes") {
			t.Fatalf("自定义按钮不应包含 Yes（原生对话框才返回 Yes）：%q", opts.Buttons)
		}
	}
	if opts.CancelButton != quitButtonCancel {
		t.Fatalf("CancelButton = %q，期望 %q", opts.CancelButton, quitButtonCancel)
	}
	if opts.DefaultButton != quitDialogDefaultButton() {
		t.Fatalf("DefaultButton = %q，期望 %q", opts.DefaultButton, quitDialogDefaultButton())
	}
	if !strings.Contains(opts.Message, "2") {
		t.Fatalf("Message 应包含实例数量，实际为 %q", opts.Message)
	}

	// 核心回归：原生对话框确认返回的 "Yes" 必须被接受，否则不会停止模拟器、也不会退出。
	if !quitSelectionConfirmed("Yes") {
		t.Fatal("原生确认框返回 Yes 时被当作取消，关闭流程会被阻止（issue #3）")
	}
}

// TestQuitDialogDefaultButton 验证默认按钮保持“取消”语义：
// 只有英文 "No" 会被 Wails 的 Windows 后端识别为默认按钮“否”。
func TestQuitDialogDefaultButton(t *testing.T) {
	got := quitDialogDefaultButton()
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(got, "no") {
			t.Fatalf("windows 下默认按钮 = %q，期望 No（MB_DEFBUTTON2）", got)
		}
		return
	}
	if got != quitButtonCancel {
		t.Fatalf("默认按钮 = %q，期望 %q", got, quitButtonCancel)
	}
}

// TestActiveEmulatorCount 验证只有运行中（含启动中/引导中）的实例才触发退出确认。
func TestActiveEmulatorCount(t *testing.T) {
	instances := []domain.EmulatorInstance{
		{State: domain.AvdRunning},
		{State: domain.AvdStarting},
		{State: domain.AvdBooting},
		{State: domain.AvdStopped},
		{State: domain.AvdStopping},
		{State: domain.AvdError},
	}
	if got := activeEmulatorCount(instances); got != 3 {
		t.Fatalf("activeEmulatorCount = %d，期望 3", got)
	}
	if got := activeEmulatorCount(nil); got != 0 {
		t.Fatalf("activeEmulatorCount(nil) = %d，期望 0", got)
	}
}
