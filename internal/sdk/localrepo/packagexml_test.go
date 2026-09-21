package localrepo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AVDDesktop/internal/sdk/repo"
)

// remoteNamespaces 模拟仓库索引根元素上的命名空间声明（实测：腾讯云/Google 一致）。
var remoteNamespaces = map[string]string{
	"sdk":      "http://schemas.android.com/sdk/android/repo/repository2/03",
	"common":   "http://schemas.android.com/repository/android/common/02",
	"generic":  "http://schemas.android.com/repository/android/generic/02",
	"sys-img2": "http://schemas.android.com/sdk/android/repo/sys-img2/01",
	"xsi":      "http://www.w3.org/2001/XMLSchema-instance",
}

// renderedPackage 是断言用的结构（按官方 package.xml 的写法解析）。
type renderedPackage struct {
	XMLName xml.Name `xml:"repository"`
	Local   struct {
		Path        string `xml:"path,attr"`
		TypeDetails struct {
			Type     string `xml:"type,attr"`
			APILevel string `xml:"api-level"`
			TagID    string `xml:"tag>id"`
			ABI      string `xml:"abi"`
		} `xml:"type-details"`
		Revision struct {
			Major   int `xml:"major"`
			Minor   int `xml:"minor"`
			Micro   int `xml:"micro"`
			Preview int `xml:"preview"`
		} `xml:"revision"`
		DisplayName string `xml:"display-name"`
		UsesLicense struct {
			Ref string `xml:"ref,attr"`
		} `xml:"uses-license"`
		Dependencies struct {
			Dependency struct {
				Path        string `xml:"path,attr"`
				MinRevision struct {
					Major int `xml:"major"`
					Minor int `xml:"minor"`
					Micro int `xml:"micro"`
				} `xml:"min-revision"`
			} `xml:"dependency"`
		} `xml:"dependencies"`
	} `xml:"localPackage"`
	// 根元素上的许可定义：sdklib 要求 uses-license 引用的 id 在本文件内定义。
	RootLicense struct {
		ID   string `xml:"id,attr"`
		Text string `xml:",chardata"`
	} `xml:"license"`
}

// TestRenderGenericPackage 验证 emulator 这类通用类型包的写法与官方一致。
func TestRenderGenericPackage(t *testing.T) {
	meta := Meta{
		Path:        "emulator",
		DisplayName: "Android Emulator",
		Revision:    Revision{Major: 37, Minor: 1, Micro: 11},
		LicenseRef:  "android-sdk-license",
		LicenseText: "Terms and Conditions of the Android SDK",
		// 索引里 xsi:type 用的是 generic 前缀（声明在根元素上）
		TypeDetails: `<type-details xsi:type="generic:genericDetailsType"/>`,
		Namespaces:  remoteNamespaces,
	}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := string(data)
	// 前缀必须被改写成官方固定前缀 ns5（generic/02），且自带 xsi 声明
	if !strings.Contains(out, `xmlns:ns5="http://schemas.android.com/repository/android/generic/02"`) {
		t.Errorf("缺少 ns5 命名空间声明：\n%s", out)
	}
	if !strings.Contains(out, `xsi:type="ns5:genericDetailsType"`) {
		t.Errorf("xsi:type 前缀未改写：\n%s", out)
	}
	if !strings.Contains(out, `xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`) {
		t.Errorf("type-details 未声明 xsi：\n%s", out)
	}

	got := decodeRendered(t, data)
	if got.Local.Path != "emulator" {
		t.Errorf("path=%q", got.Local.Path)
	}
	if got.Local.DisplayName != "Android Emulator" {
		t.Errorf("display-name=%q", got.Local.DisplayName)
	}
	if got.Local.Revision.Major != 37 || got.Local.Revision.Minor != 1 || got.Local.Revision.Micro != 11 {
		t.Errorf("revision=%+v", got.Local.Revision)
	}
	if got.Local.UsesLicense.Ref != "android-sdk-license" {
		t.Errorf("uses-license=%q", got.Local.UsesLicense.Ref)
	}
	if got.Local.TypeDetails.Type != "ns5:genericDetailsType" {
		t.Errorf("xsi:type 解析异常：%q", got.Local.TypeDetails.Type)
	}
	// 许可必须同文件定义并带文本，否则 sdklib 会以
	// `package.xml parsing problem. 未知 ID "android-sdk-license"` 丢掉整个包。
	if got.RootLicense.ID != "android-sdk-license" {
		t.Errorf("缺少 <license id=\"android-sdk-license\"> 定义：\n%s", out)
	}
	if !strings.Contains(got.RootLicense.Text, "Terms and Conditions") {
		t.Errorf("许可文本未写入：%q", got.RootLicense.Text)
	}
}

// TestRenderLicenseWithoutText 有 ref 但拿不到文本时也要写定义（否则官方工具丢包）。
func TestRenderLicenseWithoutText(t *testing.T) {
	data, err := Render(Meta{Path: "platform-tools", Revision: Revision{Major: 37}, LicenseRef: "license-2AB95128"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := decodeRendered(t, data)
	if got.RootLicense.ID != "license-2AB95128" {
		t.Errorf("缺少 <license> 定义：\n%s", data)
	}
}

// TestApplyIndexFillsLicenseAndNamespaces 索引带命名空间与许可文本时要能补全。
func TestApplyIndexFillsLicenseAndNamespaces(t *testing.T) {
	meta := Meta{LicenseRef: "android-sdk-license"}
	idx := &repo.Index{
		Namespaces: remoteNamespaces,
		Licenses: map[string]repo.License{
			"android-sdk-license": {ID: "android-sdk-license", Type: "text", Text: "SDK License"},
		},
	}
	meta.ApplyIndex(idx)
	if meta.Namespaces["generic"] == "" || meta.LicenseText != "SDK License" {
		t.Fatalf("ApplyIndex 未补全: %+v", meta)
	}
	// nil 索引不能 panic（离线路径）
	var empty Meta
	empty.ApplyIndex(nil)
}

// TestRenderSystemImage 验证系统镜像包的专有字段与依赖（emulator 最低版本）被保留。
func TestRenderSystemImage(t *testing.T) {
	min := Revision{Major: 33, Minor: 1, Micro: 19}
	meta := Meta{
		Path:        "system-images;android-34;android-desktop;x86_64",
		DisplayName: "Desktop Intel x86_64 Atom System Image",
		Revision:    Revision{Major: 1},
		LicenseRef:  "android-sdk-license",
		Dependencies: []Dependency{
			{Path: "emulator", MinRevision: &min},
		},
		// 索引里系统镜像的 type-details 用 sys-img2 前缀（老版本 schema）
		TypeDetails: `<type-details xsi:type="sys-img2:sysImgDetailsType">` +
			`<api-level>34</api-level><tag><id>android-desktop</id><display>Desktop</display></tag>` +
			`<abi>x86_64</abi></type-details>`,
		Namespaces: remoteNamespaces,
	}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := string(data)
	// sys-img2 族的最新版本是 ns14（sys-img2/05），与官方 platforms/android-36 的写法一致
	if !strings.Contains(out, `xsi:type="ns14:sysImgDetailsType"`) {
		t.Errorf("xsi:type 未归一到族最新版本：\n%s", out)
	}
	got := decodeRendered(t, data)
	td := got.Local.TypeDetails
	if td.APILevel != "34" || td.TagID != "android-desktop" || td.ABI != "x86_64" {
		t.Errorf("type-details 内容丢失: api=%q tag=%q abi=%q", td.APILevel, td.TagID, td.ABI)
	}
	dep := got.Local.Dependencies.Dependency
	if dep.Path != "emulator" || dep.MinRevision.Major != 33 || dep.MinRevision.Minor != 1 || dep.MinRevision.Micro != 19 {
		t.Errorf("依赖写错: path=%q min=%+v", dep.Path, dep.MinRevision)
	}
}

// TestRenderPreviewRevision 验证预览版本必须写 <preview>（否则官方工具当成正式版）。
func TestRenderPreviewRevision(t *testing.T) {
	meta := Meta{Path: "ndk;29.0.13113456", Revision: ParseRevision("29.0.13113456 rc1")}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := decodeRendered(t, data)
	rev := got.Local.Revision
	if rev.Major != 29 || rev.Micro != 13113456 || rev.Preview != 1 {
		t.Errorf("预览版本渲染错误: %+v", rev)
	}
}

// TestRenderRejectsEmptyPath 不允许写出没有包路径的元数据。
func TestRenderRejectsEmptyPath(t *testing.T) {
	if _, err := Render(Meta{}); err == nil {
		t.Fatal("空包路径应当报错")
	}
}

// TestFromDirReadsProperties 验证离线修复路径能从 source.properties 还原元数据。
func TestFromDirReadsProperties(t *testing.T) {
	dir := t.TempDir()
	props := "Pkg.UserSrc=false\nPkg.Revision=37.1.11\nPkg.Path=emulator\n" +
		"Pkg.Desc=Android Emulator\nPkg.BuildId=15917651\n"
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := FromDir("emulator", dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	if meta.DisplayName != "Android Emulator" || meta.Revision.Major != 37 || meta.Revision.Micro != 11 {
		t.Errorf("元数据还原错误: %+v", meta)
	}
	// 通用的离线包不该带 type-details，由 Render 补通用类型
	if meta.TypeDetails != "" {
		t.Errorf("离线元数据不应自带 type-details: %q", meta.TypeDetails)
	}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(data), `xsi:type="ns5:genericDetailsType"`) {
		t.Errorf("离线兜底应使用通用类型：\n%s", data)
	}
}

// TestRenderSystemImageInjectsAbis 镜像站索引缺 <abis> 时必须自动补齐。
//
// 实测（avdmanager 20.0）：缺 <abis> 时 sdklib 以空消息抛异常，
// avdmanager 只打印一行 "null" 且创建失败。
func TestRenderSystemImageInjectsAbis(t *testing.T) {
	meta := Meta{
		Path:        "system-images;android-34;android-desktop;x86_64",
		Revision:    Revision{Major: 1},
		ABI:         "x86_64",
		TypeDetails: `<type-details xsi:type="sys-img2:sysImgDetailsType"><api-level>34</api-level><abi>x86_64</abi></type-details>`,
		Namespaces:  remoteNamespaces,
	}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "<abi>x86_64</abi><abis>x86_64</abis>") {
		t.Errorf("未补齐 <abis>：\n%s", out)
	}
	// 已经有 <abis> 的不能重复插入
	meta.TypeDetails = `<type-details xsi:type="sys-img2:sysImgDetailsType"><abi>x86_64</abi><abis>x86_64</abis></type-details>`
	data2, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if n := strings.Count(string(data2), "<abis>"); n != 1 {
		t.Errorf("<abis> 重复插入 %d 次：\n%s", n, data2)
	}
}

// TestFromDirSystemImage 系统镜像可完全离线补写（镜像站索引可能不可用或字段缺失）。
func TestFromDirSystemImage(t *testing.T) {
	dir := t.TempDir()
	props := "Pkg.Desc=System Image x86_64.\nPkg.Revision=1\nPkg.Dependencies=emulator#33.1.19\n" +
		"AndroidVersion.ApiLevel=34\nAndroidVersion.IsBaseSdk=true\n" +
		"SystemImage.Abi=x86_64\nSystemImage.TagId=android-desktop\nSystemImage.TagDisplay=Desktop\n" +
		"Addon.VendorId=google\nAddon.VendorDisplay=Google Inc.\n"
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := FromDir("system-images;android-34;android-desktop;x86_64", dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	for _, want := range []string{
		`xsi:type="ns14:sysImgDetailsType"`,
		"<api-level>34</api-level>",
		"<base-extension>true</base-extension>",
		"<tag><id>android-desktop</id><display>Desktop</display></tag>",
		"<vendor><id>google</id><display>Google Inc.</display></vendor>",
		"<abi>x86_64</abi><abis>x86_64</abis>",
	} {
		if !strings.Contains(meta.TypeDetails, want) {
			t.Errorf("type-details 缺少 %q：\n%s", want, meta.TypeDetails)
		}
	}
	data, err := Render(meta)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := decodeRendered(t, data)
	if got.Local.TypeDetails.APILevel != "34" || got.Local.TypeDetails.TagID != "android-desktop" {
		t.Errorf("渲染后字段丢失: %+v", got.Local.TypeDetails)
	}
}

// TestFromDirKeepsDependencyMinRevision source.properties 里的 emulator#33.1.19 要变成 min-revision。
func TestFromDirKeepsDependencyMinRevision(t *testing.T) {
	dir := t.TempDir()
	props := "Pkg.Revision=1\nPkg.Desc=Desktop Image\nPkg.Dependencies=emulator#33.1.19\n"
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := FromDir("emulator", dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	if len(meta.Dependencies) != 1 || meta.Dependencies[0].Path != "emulator" {
		t.Fatalf("依赖解析错误: %+v", meta.Dependencies)
	}
	if got := meta.Dependencies[0].MinRevision; got == nil || got.Minor != 1 || got.Micro != 19 {
		t.Errorf("最低版本解析错误: %+v", got)
	}
}

// TestFromDirRequiresIndexForPlatform 平台/镜像类包离线无法还原类型，必须报错让调用方走索引。
func TestFromDirRequiresIndexForPlatform(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.properties"), []byte("Pkg.Revision=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FromDir("platforms;android-36", dir); err == nil {
		t.Fatal("platforms 包离线修复应当报错（type-details 无法还原）")
	}
	if _, err := FromDir("emulator", t.TempDir()); err == nil {
		t.Fatal("缺少 source.properties 应当报错")
	}
}

// TestFromPackageKeepsTypeDetails 由索引条目生成元数据时要带上原始 type-details 与依赖。
func TestFromPackageKeepsTypeDetails(t *testing.T) {
	pkg := repo.Package{
		Path:        "emulator",
		DisplayName: "Android Emulator",
		Revision:    repo.Revision{Major: 37, Minor: 1, Micro: 11},
		UsesLicense: repo.UsesLicense{Ref: "android-sdk-license"},
		TypeDetails: &repo.RawElement{XML: `<type-details xsi:type="generic:genericDetailsType"/>`},
		Dependencies: []repo.Dependency{{
			Path:        "emulator#33.1.19",
			MinRevision: &repo.Revision{Major: 33, Minor: 1, Micro: 19},
		}},
	}
	meta := FromPackage(pkg)
	if meta.Path != "emulator" || meta.Revision.Micro != 11 || meta.LicenseRef != "android-sdk-license" {
		t.Fatalf("元数据错误: %+v", meta)
	}
	if !strings.Contains(meta.TypeDetails, `xsi:type="generic:genericDetailsType"`) {
		t.Fatalf("type-details 未保留: %q", meta.TypeDetails)
	}
	if len(meta.Dependencies) != 1 || meta.Dependencies[0].Path != "emulator" {
		t.Fatalf("依赖路径未规范化: %+v", meta.Dependencies)
	}
	if got := meta.Dependencies[0].MinRevision; got == nil || got.Micro != 19 {
		t.Errorf("索引里的 min-revision 丢失: %+v", got)
	}
}

// TestParseRevision 版本字符串解析。
func TestParseRevision(t *testing.T) {
	cases := []struct {
		in   string
		want Revision
	}{
		{"37.1.11", Revision{Major: 37, Minor: 1, Micro: 11}},
		{"36.1", Revision{Major: 36, Minor: 1}},
		{"2", Revision{Major: 2}},
		{"29.0.13113456 rc1", Revision{Major: 29, Micro: 13113456, Preview: 1}},
		{"16.0-rc02", Revision{Major: 16, Preview: 2}},
		{"", Revision{}},
		{"garbage", Revision{}},
	}
	for _, c := range cases {
		if got := ParseRevision(c.in); got != c.want {
			t.Errorf("ParseRevision(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

// TestWriteAndHasMeta 写入落盘后可被识别（保证修复路径幂等）。
func TestWriteAndHasMeta(t *testing.T) {
	dir := t.TempDir()
	if HasMeta(dir) {
		t.Fatal("空目录不应有 package.xml")
	}
	path, err := Write(dir, Meta{Path: "platform-tools", Revision: Revision{Major: 37, Micro: 1}})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if want := filepath.Join(dir, FileName); path != want {
		t.Errorf("写入路径 %q，期望 %q", path, want)
	}
	if !HasMeta(dir) {
		t.Fatal("写完后应当检测到 package.xml")
	}
	if _, err := Write(dir, Meta{Path: "platform-tools", Revision: Revision{Major: 37, Micro: 1}}); err != nil {
		t.Fatalf("重复写入失败: %v", err)
	}
}

func decodeRendered(t *testing.T, data []byte) renderedPackage {
	t.Helper()
	var got renderedPackage
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatalf("生成的 package.xml 无法解析: %v\n%s", err, data)
	}
	return got
}
