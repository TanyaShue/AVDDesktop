package presenter

import "testing"

func TestRGBAToBGRA(t *testing.T) {
	input := []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	got, err := rgbaToBGRA(input, 2, 2)
	if err != nil {
		t.Fatalf("rgbaToBGRA failed: %v", err)
	}
	want := []byte{
		3, 2, 1, 4,
		7, 6, 5, 8,
		11, 10, 9, 12,
		15, 14, 13, 16,
	}
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRGBAToBGRAAllowsTrailingStorage(t *testing.T) {
	got, err := rgbaToBGRA([]byte{1, 2, 3, 4, 99}, 1, 1)
	if err != nil {
		t.Fatalf("rgbaToBGRA failed: %v", err)
	}
	if len(got) != 4 || got[0] != 3 || got[1] != 2 || got[2] != 1 || got[3] != 4 {
		t.Fatalf("rgbaToBGRA = %v, want [3 2 1 4]", got)
	}
}

func TestRGBAToBGRARejectsInvalidFrames(t *testing.T) {
	if _, err := rgbaToBGRA(nil, 1, 1); err == nil {
		t.Fatal("expected short frame error")
	}
	if _, err := rgbaToBGRA([]byte{1, 2, 3, 4}, 0, 1); err == nil {
		t.Fatal("expected invalid size error")
	}
}

func TestCheckedPixelBytes(t *testing.T) {
	got, ok := checkedPixelBytes(4, 3)
	if !ok || got != 48 {
		t.Fatalf("checkedPixelBytes = (%d,%v), want (48,true)", got, ok)
	}
	if _, ok := checkedPixelBytes(0, 3); ok {
		t.Fatal("zero width should be rejected")
	}
}
