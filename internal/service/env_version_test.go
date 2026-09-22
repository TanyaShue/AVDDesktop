package service

import "testing"

func TestParseEmulatorVersion(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "跳过崩溃上报 INFO 行",
			out: "INFO | Using crash reporting configuration from environment variable: ANDROID_EMU_ENABLE_CRASH_REPORTING=0\n" +
				"INFO | Android emulator version 35.6.11.0 (build_id 12891996) (CL:N/A)\n" +
				"INFO | Graphics backend: gfxstream\n",
			want: "35.6.11.0",
		},
		{
			name: "标准版本行",
			out:  "Android emulator version 36.1.9.0 (build_id 12345678)\n",
			want: "36.1.9.0",
		},
		{
			name: "仅有日志时不把 INFO 当版本",
			out:  "INFO | Using crash reporting configuration from environment variable: ANDROID_EMU_ENABLE_CRASH_REPORTING=0\n",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseEmulatorVersion(tc.out); got != tc.want {
				t.Fatalf("parseEmulatorVersion() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestParseJavaVersionAndMajor(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		version string
		major   int
		ok      bool
	}{
		{name: "Temurin 21", out: "openjdk version \"21.0.12.1\" 2026-08-21\nOpenJDK Runtime Environment Temurin-21.0.12.1+1 (build 21.0.12.1+1-LTS)\n", version: "21.0.12.1", major: 21, ok: true},
		{name: "JDK 17", out: "openjdk version \"17.0.6\" 2023-01-17\n", version: "17.0.6", major: 17, ok: true},
		{name: "旧版 1.8", out: "java version \"1.8.0_392\"\n", version: "8.0", major: 8, ok: false},
		{name: "无法解析", out: "not a java version\n", version: "not a java version", major: 0, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseJavaVersion(tc.out)
			if got != tc.version {
				t.Fatalf("parseJavaVersion() = %q，期望 %q", got, tc.version)
			}
			if gotMajor := javaMajor(got); gotMajor != tc.major {
				t.Fatalf("javaMajor(%q) = %d，期望 %d", got, gotMajor, tc.major)
			}
			if ok := javaMajor(got) >= 17; ok != tc.ok {
				t.Fatalf("Java 主版本 %d 的可用性 = %v，期望 %v", javaMajor(got), ok, tc.ok)
			}
		})
	}
}

func TestParseSdkmanagerVersion(t *testing.T) {
	out := "WARNING: The SDK Manager CLI tool (sdkmanager) is deprecated. Android CLI will be used instead.\n" +
		"The 'android' binary can also be found in the cmdline-tools directory.\n\n" +
		"1.0.16261425 (Android CLI)\n"
	if got := parseSdkmanagerVersion(out); got != "1.0.16261425" {
		t.Fatalf("parseSdkmanagerVersion() = %q，期望 1.0.16261425", got)
	}
	if got := parseSdkmanagerVersion("19.0\n"); got != "19.0" {
		t.Fatalf("旧版版本号解析错误: %q", got)
	}
}
