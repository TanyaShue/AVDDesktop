package adb

import "testing"

// TestParseDevices 解析真实的 `adb devices -l` 输出样本。
func TestParseDevices(t *testing.T) {
	out := "List of devices attached\r\n" +
		"* daemon not running; starting now at tcp:5037\r\n" +
		"* daemon started successfully\r\n" +
		"emulator-5554          device product:sdk_gphone64_x86_64 model:sdk_gphone64_x86_64 device:emu64xa transport_id:1\r\n" +
		"emulator-5556          offline transport_id:2\r\n" +
		"R58M12ABCDE            unauthorized usb:1-1 transport_id:3\r\n"

	devices := ParseDevices(out)
	if len(devices) != 3 {
		t.Fatalf("应解析出 3 个设备，实际 %d：%+v", len(devices), devices)
	}

	first := devices[0]
	if first.Serial != "emulator-5554" || first.State != "device" || !first.IsEmulator {
		t.Errorf("第一个设备解析错误：%+v", first)
	}
	if first.Product != "sdk_gphone64_x86_64" || first.Model != "sdk_gphone64_x86_64" ||
		first.Device != "emu64xa" || first.TransportID != "1" {
		t.Errorf("第一个设备的详细字段解析错误：%+v", first)
	}

	if second := devices[1]; second.State != "offline" || !second.IsEmulator {
		t.Errorf("第二个设备解析错误：%+v", second)
	}
	if third := devices[2]; third.Serial != "R58M12ABCDE" || third.State != "unauthorized" || third.IsEmulator {
		t.Errorf("第三个设备解析错误：%+v", third)
	}
}

// TestParseDevicesEmpty 空输出不应产生设备。
func TestParseDevicesEmpty(t *testing.T) {
	if devices := ParseDevices("List of devices attached\n\n"); len(devices) != 0 {
		t.Fatalf("空输出应解析出 0 个设备：%+v", devices)
	}
}
