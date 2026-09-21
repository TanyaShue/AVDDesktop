package repo

import (
	"strings"
	"testing"
)

// testIndex 是一份最小但结构真实的索引片段（含 type-details / 预览版本 / 依赖最低版本）。
const testIndex = `<?xml version='1.0' encoding='utf-8'?>
<sdk:sdk-repository xmlns:sdk="http://schemas.android.com/sdk/android/repo/repository2/03"
    xmlns:common="http://schemas.android.com/repository/android/common/02"
    xmlns:generic="http://schemas.android.com/repository/android/generic/02"
    xmlns:sys-img2="http://schemas.android.com/sdk/android/repo/sys-img2/01"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <license id="android-sdk-license" type="text">Terms…</license>
  <remotePackage path="emulator">
    <type-details xsi:type="generic:genericDetailsType"/>
    <revision><major>37</major><minor>1</minor><micro>11</micro></revision>
    <display-name>Android Emulator</display-name>
    <uses-license ref="android-sdk-license"/>
    <archives>
      <archive>
        <complete><size>441975345</size><checksum type="sha1">abc</checksum><url>emulator-windows_x64-15917651.zip</url></complete>
        <host-os>windows</host-os>
        <host-arch>x64</host-arch>
      </archive>
    </archives>
  </remotePackage>
  <remotePackage path="ndk;29.0.13113456">
    <type-details xsi:type="generic:genericDetailsType"/>
    <revision><major>29</major><minor>0</minor><micro>13113456</micro><preview>1</preview></revision>
    <display-name>NDK (Side by side) 29.0.13113456 rc1</display-name>
  </remotePackage>
  <remotePackage path="system-images;android-34;android-desktop;x86_64">
    <type-details xsi:type="sys-img2:sysImgDetailsType"><api-level>34</api-level>
      <tag><id>android-desktop</id><display>Desktop</display></tag>
      <abi>x86_64</abi>
    </type-details>
    <revision><major>1</major></revision>
    <display-name>Desktop Intel x86_64 Atom System Image</display-name>
    <dependencies><dependency path="emulator"><min-revision><major>33</major><minor>1</minor><micro>19</micro></min-revision></dependency></dependencies>
  </remotePackage>
</sdk:sdk-repository>`

// TestParseIndexKeepsTypeDetails 验证 type-details 原始 XML 与结构化字段都能拿到。
func TestParseIndexKeepsTypeDetails(t *testing.T) {
	idx, err := ParseIndex([]byte(testIndex), "https://mirror.example.com/AndroidSDK/repository2-3.xml")
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}

	// 根元素命名空间声明（写本地 package.xml 时要用它改写 xsi:type 前缀）
	if got := idx.Namespaces["generic"]; got != "http://schemas.android.com/repository/android/generic/02" {
		t.Errorf("Namespaces[generic] = %q", got)
	}

	emu, ok := idx.Find("emulator")
	if !ok {
		t.Fatal("找不到 emulator 包")
	}
	if emu.TypeDetails == nil || !strings.Contains(emu.TypeDetails.XML, `xsi:type="generic:genericDetailsType"`) {
		t.Fatalf("type-details 原始 XML 未保留: %+v", emu.TypeDetails)
	}
	if emu.Revision.String() != "37.1.11" {
		t.Errorf("revision = %s", emu.Revision.String())
	}

	img, ok := idx.Find("system-images;android-34;android-desktop;x86_64")
	if !ok {
		t.Fatal("找不到系统镜像包")
	}
	if img.APILevel != "34" || img.TagID != "android-desktop" || img.TagDisplay != "Desktop" || img.ABI != "x86_64" {
		t.Errorf("type-details 字段解析异常: api=%q tag=%q display=%q abi=%q",
			img.APILevel, img.TagID, img.TagDisplay, img.ABI)
	}
	if len(img.Dependencies) != 1 || img.Dependencies[0].Path != "emulator" {
		t.Fatalf("依赖解析异常: %+v", img.Dependencies)
	}
	min := img.Dependencies[0].MinRevision
	if min == nil || min.Major != 33 || min.Minor != 1 || min.Micro != 19 {
		t.Errorf("min-revision 解析异常: %+v", min)
	}

	ndk, ok := idx.Find("ndk;29.0.13113456")
	if !ok {
		t.Fatal("找不到 ndk 包")
	}
	if ndk.Revision.Preview != 1 {
		t.Errorf("预览标记解析异常: %+v", ndk.Revision)
	}
}

// TestEnsureXSINamespace 独立片段必须自带 xsi 声明（否则前缀无处解析）。
func TestEnsureXSINamespace(t *testing.T) {
	raw := `<type-details xsi:type="generic:genericDetailsType"/>`
	got := EnsureXSINamespace(raw)
	if !strings.Contains(got, `xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`) {
		t.Fatalf("未补 xsi 声明: %s", got)
	}
	if again := EnsureXSINamespace(got); strings.Count(again, "xmlns:xsi") != 1 {
		t.Fatalf("重复补声明: %s", again)
	}
	if got := EnsureXSINamespace(""); got != "" {
		t.Errorf("空输入应返回空，实际 %q", got)
	}
}
