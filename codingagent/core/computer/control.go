package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Result is one computer action plus a fresh screenshot of the main display.
type Result struct {
	Text   string
	JPEG   []byte
	Width  int
	Height int
}

var actionMu sync.Mutex

var prompted sync.Once

func trusted() bool {
	if accessibilityTrusted(false) {
		return true
	}
	prompted.Do(func() { accessibilityTrusted(true) })
	return accessibilityTrusted(false)
}

// Available reports whether this process can drive the local desktop.
func Available() bool { return runtime.GOOS == "darwin" }

// Act runs one computer action and returns a screenshot taken afterward.
// Actions are serialized so the pointer is never driven from two calls at once.
func Act(ctx context.Context, name string, args map[string]any) (Result, error) {
	if !Available() {
		return Result{}, fmt.Errorf("computer use requires macOS")
	}
	actionMu.Lock()
	defer actionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ensureOverlay()
	chatSay("act", name)
	if !trusted() {
		return Result{}, fmt.Errorf("Accessibility permission is not granted. Enable maiku in System Settings → Privacy & Security → Accessibility, and allow Screen Recording")
	}
	text, err := run(ctx, name, args)
	if err != nil {
		return Result{}, err
	}
	img, err := screenshot(true)
	if err != nil {
		return Result{}, err
	}
	w, h := screenSize()
	x, y := agentPos()
	note := fmt.Sprintf("%s\nScreenshot %dx%d. Cursor at (%d, %d). Coordinates are pixels from the top-left of this image.", text, w, h, int(x), int(y))
	return Result{Text: note, JPEG: img, Width: w, Height: h}, nil
}

func run(ctx context.Context, name string, args map[string]any) (string, error) {
	if name == "batch" {
		return runBatch(ctx, args)
	}
	w, h := screenSize()
	inBounds := func(x, y int) bool { return x >= 0 && y >= 0 && x < w && y < h }
	ix := argInt(args, "x")
	iy := argInt(args, "y")

	switch name {
	case "click":
		if !inBounds(ix, iy) {
			return "", fmt.Errorf("(%d,%d) outside screen %dx%d", ix, iy, w, h)
		}
		button := argString(args, "button")
		if button == "" {
			button = "left"
		}
		clicks := argInt(args, "clicks")
		if clicks < 1 {
			clicks = 1
		}
		if clicks > 3 {
			clicks = 3
		}
		click(float64(ix), float64(iy), button, clicks)
		return fmt.Sprintf("%s-click ×%d at (%d, %d)", button, clicks, ix, iy), nil
	case "move":
		if !inBounds(ix, iy) {
			return "", fmt.Errorf("(%d,%d) outside screen %dx%d", ix, iy, w, h)
		}
		hover(float64(ix), float64(iy))
		return fmt.Sprintf("moved to (%d, %d)", ix, iy), nil
	case "drag":
		x1, y1, x2, y2 := argInt(args, "x1"), argInt(args, "y1"), argInt(args, "x2"), argInt(args, "y2")
		if !inBounds(x1, y1) || !inBounds(x2, y2) {
			return "", fmt.Errorf("drag endpoints outside screen %dx%d", w, h)
		}
		drag(float64(x1), float64(y1), float64(x2), float64(y2))
		return fmt.Sprintf("dragged (%d, %d) → (%d, %d)", x1, y1, x2, y2), nil
	case "type":
		text := argString(args, "text")
		x, y := agentPos()
		setCrosshair(x, y, false, "typing…")
		typeText(text)
		return "typed text", nil
	case "key":
		keys := argString(args, "keys")
		x, y := agentPos()
		setCrosshair(x, y, false, "key "+keys)
		if err := pressKeys(keys); err != nil {
			return "", err
		}
		return "pressed " + keys, nil
	case "scroll":
		scroll(float64(ix), float64(iy), argInt(args, "dx"), argInt(args, "dy"))
		return fmt.Sprintf("scrolled at (%d, %d)", ix, iy), nil
	case "wait":
		seconds := argInt(args, "seconds")
		if seconds < 1 {
			seconds = 1
		}
		if seconds > 10 {
			seconds = 10
		}
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
		return fmt.Sprintf("waited %ds", seconds), nil
	case "screenshot":
		return "screenshot", nil
	default:
		return "", fmt.Errorf("unknown action %q", name)
	}
}

func runBatch(ctx context.Context, args map[string]any) (string, error) {
	raw, ok := args["actions"].([]any)
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("actions is empty")
	}
	for i, item := range raw {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		step, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("action %d is not an object", i+1)
		}
		action := argString(step, "action")
		switch action {
		case "batch", "screenshot", "":
			return "", fmt.Errorf("action %d: %q is not allowed inside batch", i+1, action)
		}
		if _, err := run(ctx, action, step); err != nil {
			return "", fmt.Errorf("completed %d/%d; action %d (%s) failed: %w", i, len(raw), i+1, action, err)
		}
		if delay := argInt(step, "delay_ms"); delay > 0 {
			if delay > 10000 {
				delay = 10000
			}
			timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Sprintf("batch of %d actions", len(raw)), nil
}

var agentX, agentY = math.NaN(), math.NaN()

func agentPos() (float64, float64) {
	if math.IsNaN(agentX) {
		w, h := screenSize()
		agentX, agentY = float64(w)/2, float64(h)/2
	}
	return agentX, agentY
}

func glide(x, y float64, dragButton int, status string) {
	sx, sy := agentPos()
	dist := math.Hypot(x-sx, y-sy)
	steps := int(math.Min(40, math.Max(8, dist/25)))
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		t = t * t * (3 - 2*t)
		cx, cy := sx+(x-sx)*t, sy+(y-sy)*t
		if dragButton != 0 {
			mouseMove(cx, cy, dragButton)
		}
		setCrosshair(cx, cy, dragButton != 0, status)
		time.Sleep(12 * time.Millisecond)
	}
	agentX, agentY = x, y
}

func borrowCursor(x, y float64, fn func()) {
	ux, uy := cursorPos()
	warpCursor(x, y)
	mouseMove(x, y, 0)
	time.Sleep(15 * time.Millisecond)
	fn()
	warpCursor(ux, uy)
}

func hover(x, y float64) {
	glide(x, y, 0, "move")
	borrowCursor(x, y, func() { time.Sleep(30 * time.Millisecond) })
}

func buttonID(name string) int {
	switch name {
	case "right":
		return 2
	case "middle":
		return 3
	}
	return 1
}

func click(x, y float64, button string, clicks int) {
	glide(x, y, 0, fmt.Sprintf("%s click ×%d", button, clicks))
	b := buttonID(button)
	setCrosshair(x, y, true, "")
	borrowCursor(x, y, func() {
		for i := 1; i <= clicks; i++ {
			mouseButton(x, y, b, 1, i)
			time.Sleep(40 * time.Millisecond)
			mouseButton(x, y, b, 0, i)
			if i < clicks {
				time.Sleep(60 * time.Millisecond)
			}
		}
	})
	time.Sleep(80 * time.Millisecond)
	setCrosshair(x, y, false, "")
}

func drag(x1, y1, x2, y2 float64) {
	glide(x1, y1, 0, "drag")
	borrowCursor(x1, y1, func() {
		mouseButton(x1, y1, 1, 1, 1)
		time.Sleep(80 * time.Millisecond)
		glide(x2, y2, 1, "dragging")
		time.Sleep(80 * time.Millisecond)
		mouseButton(x2, y2, 1, 0, 1)
	})
	setCrosshair(x2, y2, false, "")
}

func scroll(x, y float64, dx, dy int) {
	glide(x, y, 0, "scroll")
	borrowCursor(x, y, func() {
		for i := 0; i < max(abs(dx), abs(dy)); i++ {
			scrollWheel(-sign(dy)*btoi(i < abs(dy)), -sign(dx)*btoi(i < abs(dx)))
			time.Sleep(15 * time.Millisecond)
		}
	})
}

func typeText(s string) {
	for _, r := range s {
		if r == '\n' {
			_ = pressKeys("enter")
			continue
		}
		u := utf16.Encode([]rune{r})
		units := make([]uint16, len(u))
		for i, v := range u {
			units[i] = uint16(v)
		}
		typeUnicode(units)
		time.Sleep(8 * time.Millisecond)
	}
}

const (
	flagShift = 0x00020000
	flagCtrl  = 0x00040000
	flagAlt   = 0x00080000
	flagCmd   = 0x00100000
)

var modifierFlags = map[string]uint64{
	"shift": flagShift, "ctrl": flagCtrl, "control": flagCtrl,
	"alt": flagAlt, "option": flagAlt, "opt": flagAlt,
	"cmd": flagCmd, "command": flagCmd, "super": flagCmd, "meta": flagCmd, "win": flagCmd,
}

var modifierKeycodes = map[uint64]int{flagShift: 56, flagCtrl: 59, flagAlt: 58, flagCmd: 55}

var keycodes = map[string]int{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
	"b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19,
	"3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28,
	"0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "l": 37, "j": 38,
	"'": 39, "k": 40, ";": 41, "\\": 42, ",": 43, "/": 44, "n": 45, "m": 46, ".": 47,
	"`": 50, "enter": 36, "return": 36, "tab": 48, "space": 49, "backspace": 51,
	"delete": 117, "escape": 53, "esc": 53, "home": 115, "end": 119, "pageup": 116,
	"pagedown": 121, "left": 123, "right": 124, "down": 125, "up": 126,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
	"f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

func pressKeys(combo string) error {
	var flags uint64
	var mods []uint64
	key := -1
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+") {
		part = strings.TrimSpace(part)
		if f, ok := modifierFlags[part]; ok {
			flags |= f
			mods = append(mods, f)
			continue
		}
		k, ok := keycodes[part]
		if !ok {
			return fmt.Errorf("unknown key %q", part)
		}
		key = k
	}
	for _, m := range mods {
		keyEvent(modifierKeycodes[m], 1, flags)
	}
	if key >= 0 {
		keyEvent(key, 1, flags)
		time.Sleep(20 * time.Millisecond)
		keyEvent(key, 0, flags)
	}
	for i := len(mods) - 1; i >= 0; i-- {
		keyEvent(modifierKeycodes[mods[i]], 0, 0)
	}
	return nil
}

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func argInt(args map[string]any, key string) int {
	switch n := args[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
