package sdk

import (
	"strings"
	"testing"
)

const repositorySample = `<?xml version="1.0" encoding="UTF-8"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;latest">
    <display-name>Android SDK Command-line Tools (latest)</display-name>
    <archives>
      <archive>
        <complete><size>100</size><checksum type="sha1">aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</checksum><url>commandlinetools-win-1_latest.zip</url></complete>
        <host-os>windows</host-os><host-arch>x64</host-arch>
      </archive>
      <archive>
        <complete><size>90</size><checksum type="sha1">bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</checksum><url>commandlinetools-linux-1_latest.zip</url></complete>
        <host-os>linux</host-os>
      </archive>
    </archives>
  </remotePackage>
  <remotePackage path="platform-tools">
    <display-name>Android SDK Platform-Tools</display-name>
    <archives>
      <archive>
        <complete><size>200</size><checksum type="sha1">cccccccccccccccccccccccccccccccccccccccc</checksum><url>platform-tools-latest-windows.zip</url></complete>
        <host-os>windows</host-os><host-arch>x64</host-arch>
      </archive>
    </archives>
  </remotePackage>
</sdk-repository>`

func TestParseRepositoryAndMatchArchive(t *testing.T) {
	idx, err := ParseRepository([]byte(repositorySample), "https://mirror.example/android/repository/")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := idx.ArchiveFor("cmdline-tools;latest", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://mirror.example/android/repository/commandlinetools-win-1_latest.zip"; archive.URL != want {
		t.Fatalf("归档地址 = %q，期望 %q", archive.URL, want)
	}
	if archive.Size != 100 || archive.SHA1 != strings.Repeat("a", 40) {
		t.Fatalf("归档元数据错误: %+v", archive)
	}
}

func TestRepositoryArchivePreferenceAndMissingPlatform(t *testing.T) {
	idx, err := ParseRepository([]byte(repositorySample), "https://mirror.example/android/repository")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := idx.ArchiveFor("cmdline-tools;latest", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archive.URL, "commandlinetools-linux-") {
		t.Fatalf("linux 应匹配 linux 归档，实际 %q", archive.URL)
	}
	if _, err := idx.ArchiveFor("platform-tools", "linux", "amd64"); err == nil {
		t.Fatal("不存在的平台归档必须报错")
	}
}

func TestParseRepositoryRejectsHTML(t *testing.T) {
	if _, err := ParseRepository([]byte("<html>not an sdk repo</html>"), "https://mirror.example/"); err == nil {
		t.Fatal("普通 HTML 不应被当成仓库索引")
	}
}

func TestWithBaseURLReplacesExistingValue(t *testing.T) {
	env := []string{"PATH=C:\\Windows", "sdk_test_base_url=https://old.example/"}
	got := WithBaseURL(env, "https://mirror.example/android/repository")
	count := 0
	for _, item := range got {
		if len(item) >= len("SDK_TEST_BASE_URL=") && item[:len("SDK_TEST_BASE_URL=")] == "SDK_TEST_BASE_URL=" {
			count++
			if item != "SDK_TEST_BASE_URL=https://mirror.example/android/repository/" {
				t.Fatalf("镜像环境变量错误: %q", item)
			}
		}
	}
	if count != 1 {
		t.Fatalf("SDK_TEST_BASE_URL 数量 = %d，期望 1: %#v", count, got)
	}
}
