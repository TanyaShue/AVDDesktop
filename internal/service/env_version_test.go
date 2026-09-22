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
