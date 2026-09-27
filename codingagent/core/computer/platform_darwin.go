//go:build darwin

package computer

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include <stdlib.h>
#include "platform_darwin.h"
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"
)

func ensureOverlay() { C.OverlayEnsure() }

func accessibilityTrusted(prompt bool) bool {
	p := 0
	if prompt {
		p = 1
	}
	return C.AccessibilityTrusted(C.int(p)) != 0
}

func screenSize() (int, int) {
	var w, h C.double
	C.ScreenSize(&w, &h)
	return int(w), int(h)
}

func cursorPos() (float64, float64) {
	var x, y C.double
	C.CursorPos(&x, &y)
	return float64(x), float64(y)
}

func chatSay(role, text string) {
	if text == "" {
		return
	}
	cr, ct := C.CString(role), C.CString(text)
	defer C.free(unsafe.Pointer(cr))
	defer C.free(unsafe.Pointer(ct))
	C.ChatPost(cr, ct)
}

func setCrosshair(x, y float64, pressed bool, status string) {
	var cs *C.char
	if status != "" {
		cs = C.CString(status)
		defer C.free(unsafe.Pointer(cs))
	}
	p := 0
	if pressed {
		p = 1
	}
	C.OverlaySet(C.double(x), C.double(y), C.int(p), cs)
}

func warpCursor(x, y float64) { C.WarpCursor(C.double(x), C.double(y)) }

func mouseMove(x, y float64, dragButton int) {
	C.MouseMove(C.double(x), C.double(y), C.int(dragButton))
}

func mouseButton(x, y float64, button, down, clicks int) {
	C.MouseButton(C.double(x), C.double(y), C.int(button), C.int(down), C.int(clicks))
}

func scrollWheel(dy, dx int) { C.Scroll(C.int(dy), C.int(dx)) }

func keyEvent(keycode, down int, flags uint64) {
	C.KeyEvent(C.int(keycode), C.int(down), C.ulonglong(flags))
}

func typeUnicode(units []uint16) {
	if len(units) == 0 {
		return
	}
	C.TypeUnicode((*C.ushort)(unsafe.Pointer(&units[0])), C.int(len(units)))
}

func screenshot(hideCrosshair bool) ([]byte, error) {
	if hideCrosshair {
		C.OverlayVisible(0)
		defer C.OverlayVisible(1)
		time.Sleep(30 * time.Millisecond)
	}
	f := filepath.Join(os.TempDir(), fmt.Sprintf("maiku-cua-%d.jpg", time.Now().UnixNano()))
	defer os.Remove(f)
	if out, err := exec.Command("screencapture", "-x", "-m", "-t", "jpg", f).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("screencapture: %v: %s", err, out)
	}
	w, _ := screenSize()
	if out, err := exec.Command("sips", "--resampleWidth", fmt.Sprint(w), "-s", "formatOptions", "70", f).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sips: %v: %s", err, out)
	}
	return os.ReadFile(f)
}
