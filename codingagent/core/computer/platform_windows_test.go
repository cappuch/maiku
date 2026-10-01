//go:build windows && (amd64 || arm64)

package computer

import (
	"testing"
	"unsafe"
)

func TestWindowsInputABI(t *testing.T) {
	if unsafe.Sizeof(msInput{}) != 40 {
		t.Fatalf("mouse INPUT size = %d, want 40", unsafe.Sizeof(msInput{}))
	}
	if unsafe.Sizeof(kbInput{}) != 40 {
		t.Fatalf("keyboard INPUT size = %d, want 40", unsafe.Sizeof(kbInput{}))
	}
	if unsafe.Sizeof(winMsg{}) != 48 {
		t.Fatalf("MSG size = %d, want 48", unsafe.Sizeof(winMsg{}))
	}
	if unsafe.Sizeof(wndClassEx{}) != 80 {
		t.Fatalf("WNDCLASSEX size = %d, want 80", unsafe.Sizeof(wndClassEx{}))
	}
	if unsafe.Sizeof(monitorInfo{}) != 40 {
		t.Fatalf("MONITORINFO size = %d, want 40", unsafe.Sizeof(monitorInfo{}))
	}
	if unsafe.Sizeof(bitmapInfoHeader{}) != 40 {
		t.Fatalf("BITMAPINFOHEADER size = %d, want 40", unsafe.Sizeof(bitmapInfoHeader{}))
	}
}

func TestMacKeycodesHaveWindowsVKs(t *testing.T) {
	for name, code := range keycodes {
		if _, ok := macToVK[code]; !ok {
			t.Errorf("no Windows VK for %s (%d)", name, code)
		}
	}
	for _, code := range modifierKeycodes {
		if _, ok := macToVK[code]; !ok {
			t.Errorf("no Windows VK for modifier %d", code)
		}
	}
}
