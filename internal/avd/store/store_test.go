package store

import (
	"os"
	"path/filepath"
	"testing"

	"AVDDesktop/internal/domain"
)

// TestParseRealIni 用真实 AVD 的 config.ini 验证解析（样本不存在时跳过）。
func TestParseRealIni(t *testing.T) {
	candidates := []string{
		filepath.Join(os.Getenv("ANDROID_SDK_HOME"), ".android", "avd"),
		filepath.Join(os.Getenv("USERPROFILE"), ".android", "avd"),
	}
	var home string
	for _, c := range candidates {
		if c == "" || c == filepath.Join(".android", "avd") {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			if entries, _ := os.ReadDir(c); len(entries) > 0 {
				home = c
				break
			}
		}
	}
	if home == "" {
		t.Skip("本机没有可用的 AVD 样本")
	}
	s := New(home)
	items, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Skip("AVD 目录为空")
	}
	for _, it := range items {
		t.Logf("AVD: %s | 显示名=%s | API=%s | tag=%s abi=%s | %d×%d@%d | RAM=%dMB cores=%d | 存储=%s | play=%v | broken=%q",
			it.Name, it.DisplayName, it.APILevel, it.TagID, it.ABI,
			it.Width, it.Height, it.Density, it.RAMMB, it.Cores, it.DataPartition, it.Playstore, it.Broken)
		if it.APILevel == "" {
			t.Errorf("%s: API 版本解析失败", it.Name)
		}
		if it.RAMMB == 0 || it.Width == 0 {
			t.Errorf("%s: 关键配置缺失", it.Name)
		}
	}

	// ini 往返一致性
	src := items[0]
	detail, err := s.Detail(src.Name)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := ParseIni(FormatIni(detail.Config))
	if len(roundTrip) != len(detail.Config) {
		t.Errorf("ini 往返丢键：before=%d after=%d", len(detail.Config), len(roundTrip))
	}
	for k, v := range detail.Config {
		if roundTrip[k] != v {
			t.Errorf("键 %s 值不一致: %q → %q", k, v, roundTrip[k])
		}
	}

	// 名称校验
	if r := s.ValidateName(src.Name); r.Valid {
		t.Errorf("已存在的名称应判定为非法: %s", src.Name)
	}
	if r := s.ValidateName("bad name!"); r.Valid {
		t.Error("含非法字符的名称应判定为非法")
	}
	if r := s.ValidateName("valid_name-1"); !r.Valid {
		t.Error("合法名称被误判")
	}

	// schema 覆盖度：真实 config 中的键应大多数在 Schema 中登记
	schema := SchemaByKey()
	unknown := []string{}
	for k := range detail.Config {
		if _, ok := schema[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	t.Logf("Schema 未登记的真实配置键 (%d): %v", len(unknown), unknown)
}

func TestDefaultHWAndDiff(t *testing.T) {
	hw := DefaultHW("medium_phone", 4096, 8)
	if hw["hw.ramSize"] != "4096" || hw["vm.heapSize"] != "512" {
		t.Errorf("默认值异常: ram=%s heap=%s", hw["hw.ramSize"], hw["vm.heapSize"])
	}
	before := map[string]string{"hw.ramSize": "2048", "hw.gpu.mode": "auto", "x.old": "1"}
	after := map[string]string{"hw.ramSize": "4096", "hw.gpu.mode": "auto", "hw.new": "yes"}
	diff := DiffConfig(before, after)
	if diff.Changed["hw.ramSize"] != "4096" {
		t.Error("未识别变更项")
	}
	if diff.Added["hw.new"] != "yes" {
		t.Error("未识别新增项")
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "x.old" {
		t.Errorf("未识别删除项: %v", diff.Removed)
	}
	if len(diff.Warnings) == 0 {
		t.Error("应当对未知键给出警告")
	}
}

var _ = domain.AvdSummary{}
