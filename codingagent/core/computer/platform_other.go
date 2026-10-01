//go:build !darwin && !(windows && (amd64 || arm64))

package computer

import "fmt"

func ensureOverlay() {}

func accessibilityTrusted(bool) bool { return false }

func screenSize() (int, int) { return 0, 0 }

func cursorPos() (float64, float64) { return 0, 0 }

func chatSay(string, string) {}

func setCrosshair(float64, float64, bool, string) {}

func warpCursor(float64, float64) {}

func mouseMove(float64, float64, int) {}

func mouseButton(float64, float64, int, int, int) {}

func scrollWheel(int, int) {}

func keyEvent(int, int, uint64) {}

func typeUnicode([]uint16) {}

func screenshot(bool) ([]byte, error) {
	return nil, fmt.Errorf("computer use requires macOS or Windows")
}
