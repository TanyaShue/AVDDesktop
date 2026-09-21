package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"AVDDesktop/internal/config"
	"AVDDesktop/internal/logging"
)

// TestJSONArrayContract 断言绑定方法返回的 JSON 里，数组字段不会退化成 null。
//
// 契约见 docs/API-CONTRACT.md：数组字段即使为空也必须是数组。Go 的 nil 切片会被
// encoding/json 序列化成 null，前端 `arr.length` / `arr.map(...)` 会直接抛异常，
// 而 React 在没有错误边界时会卸载整棵树 —— 表现就是整个窗口白屏。
// 真实事故：健康环境下 EnvReport.blockers=null 让首页白屏。
//
// 测试用临时目录充当 SDK/AVD（不存在），这样所有列表都是“空列表”，
// 恰好是 nil 切片最容易漏掉归一化的场景；用例本身不依赖本机环境与网络。
func TestJSONArrayContract(t *testing.T) {
	dir := t.TempDir()
	settings := config.NewManager(filepath.Join(dir, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("加载设置失败: %v", err)
	}
	if _, err := settings.Update(map[string]any{
		"sdkRoot": filepath.Join(dir, "sdk"),
		"avdHome": filepath.Join(dir, "avd"),
	}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	rt := NewRuntime("AVDDesktopContractTest", "test", settings, filepath.Join(dir, "cache"), logging.Discard())
	t.Cleanup(rt.Shutdown)

	envSvc := NewEnvService(rt)
	avdSvc := NewAvdService(rt)

	// 自检是本次事故的现场：“数组字段不能是 null”对 blockers 同样适用。
	// 注意这里的 SDK 指向临时目录，blockers 非空；
	// “健康环境 blockers 为空”的场景（真正的白屏触发条件）由
	// internal/sdk/detect 的 TestNormalizeReportJSONArrays 单独锁定。
	report, err := envSvc.Detect(DetectRequest{Force: true})
	if err != nil {
		t.Fatalf("环境自检失败: %v", err)
	}

	avds, err := avdSvc.List()
	if err != nil {
		t.Fatalf("Avd.List 调用失败: %v", err)
	}

	type payload struct {
		name string
		v    any
	}
	payloads := []payload{
		{"Env.Detect", report},
		{"Env.DetectSdkRoots", envSvc.DetectSdkRoots()},
		{"Avd.List", avds},
		{"Avd.ListConfigSchema", avdSvc.ListConfigSchema()},
		{"Mirror.ListSources", NewMirrorService(rt).ListSources()},
		{"Mirror.GetCachedResults", NewMirrorService(rt).GetCachedResults()},
		{"Emulator.ListRunning", NewEmulatorService(rt).ListRunning()},
		{"Jobs.List", NewJobService(rt).List()},
		{"Sdk.ListInstalled", NewSdkService(rt).ListInstalled()},
		{"Diagnostics.LogFiles", NewDiagnosticsService(rt).LogFiles()},
	}
	// 这两个依赖设置/外部进程，缺前置条件时跳过而不是让契约测试误报
	if images, err := NewSdkService(rt).ListSystemImages(ListSystemImagesRequest{OnlyInstalled: true}); err != nil {
		t.Logf("跳过 Sdk.ListSystemImages：%v", err)
	} else {
		payloads = append(payloads, payload{"Sdk.ListSystemImages", images})
	}
	if profiles, err := avdSvc.ListProfiles(false); err != nil {
		t.Logf("跳过 Avd.ListProfiles：%v", err)
	} else {
		payloads = append(payloads, payload{"Avd.ListProfiles", profiles})
	}

	for _, p := range payloads {
		assertNoNullArrays(t, p.name, p.v)
	}
}

// assertNoNullArrays 比对 Go 值与它的 JSON 表示：任何切片字段（含嵌套结构体与切片元素）都不允许是 null。
func assertNoNullArrays(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s 序列化失败: %v", name, err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s 反序列化失败: %v", name, err)
	}
	checkNullArrays(t, name, reflect.ValueOf(v), decoded)
}

func checkNullArrays(t *testing.T, path string, rv reflect.Value, jv any) {
	t.Helper()
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return // 指针/接口为 nil 表示字段缺省，不属于数组契约范围
		}
		checkNullArrays(t, path, rv.Elem(), jv)
	case reflect.Slice:
		if jv == nil {
			t.Errorf("%s 序列化成了 null；契约要求空数组输出 []（前端 .length 会直接抛异常）", path)
			return
		}
		arr, ok := jv.([]any)
		if !ok {
			return
		}
		for i := 0; i < rv.Len() && i < len(arr); i++ {
			checkNullArrays(t, fmt.Sprintf("%s[%d]", path, i), rv.Index(i), arr[i])
		}
	case reflect.Struct:
		fields, ok := jv.(map[string]any)
		if !ok {
			return
		}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Type().Field(i)
			if f.PkgPath != "" {
				continue // 未导出字段不参与 JSON
			}
			key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if key == "" {
				key = f.Name
			}
			if key == "-" {
				continue
			}
			child, present := fields[key]
			if !present {
				continue // omitempty 省略字段是允许的
			}
			checkNullArrays(t, path+"."+key, rv.Field(i), child)
		}
	}
}
