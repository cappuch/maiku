//go:build windows && (amd64 || arm64)

package computer

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmRun = 0x8000 + 1

	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	wsChild        = 0x40000000
	wsClipChildren = 0x02000000
	wsVScroll      = 0x00200000

	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000

	swHide   = 0
	swShowNA = 8

	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	wmSize           = 0x0005
	wmClose          = 0x0010
	wmGetTextLength  = 0x000E
	wmSetFont        = 0x0030
	wmCommand        = 0x0111
	wmCtlColorEdit   = 0x0133
	wmCtlColorBtn    = 0x0135
	wmCtlColorStatic = 0x0138
	emSetSel         = 0x00B1
	emReplaceSel     = 0x00C2
	emScrollCaret    = 0x00B7

	srcCopy                 = 0x00CC0020
	wdaExcludeFromCapture   = 0x00000011
	mouseMoveFlag           = 0x0001
	mouseLeftDown           = 0x0002
	mouseLeftUp             = 0x0004
	mouseRightDown          = 0x0008
	mouseRightUp            = 0x0010
	mouseMiddleDown         = 0x0020
	mouseMiddleUp           = 0x0040
	mouseWheel              = 0x0800
	mouseHWheel             = 0x1000
	mouseAbsolute           = 0x8000
	mouseVirtualDesk        = 0x4000
	keyEventKeyUp           = 0x0002
	keyEventUnicode         = 0x0004
	keyEventExtended        = 0x0001
	monitorDefaultToPrimary = 1
	smXVirtualScreen        = 76
	smYVirtualScreen        = 77
	smCXVirtualScreen       = 78
	smCYVirtualScreen       = 79
	logPixelsX              = 88
	idcArrow                = 32512
	nullPen                 = 8
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type winSize struct{ CX, CY int32 }
type blendFunc struct{ Op, Flags, Alpha, Format byte }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	Menu       *uint16
	Class      *uint16
	IconSm     uintptr
}

type winMsg struct {
	Hwnd     uintptr
	Message  uint32
	Pad      uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	PtX      int32
	PtY      int32
	LPrivate uint32
}

// msInput and kbInput match the 64-bit INPUT union (40 bytes).
type msInput struct {
	Type  uint32
	Pad   uint32
	Dx    int32
	Dy    int32
	Data  uint32
	Flags uint32
	Time  uint32
	Pad2  uint32
	Extra uintptr
}

type kbInput struct {
	Type  uint32
	Pad   uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Pad2  uint32
	Extra uintptr
	Tail  [8]byte
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pGetDC                    = user32.NewProc("GetDC")
	pReleaseDC                = user32.NewProc("ReleaseDC")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pMonitorFromPoint         = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfo           = user32.NewProc("GetMonitorInfoW")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pSetCursorPos             = user32.NewProc("SetCursorPos")
	pSendInput                = user32.NewProc("SendInput")
	pGetDeviceCaps            = gdi32.NewProc("GetDeviceCaps")
	pCreateCompatibleDC       = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC                 = gdi32.NewProc("DeleteDC")
	pCreateDIBSection         = gdi32.NewProc("CreateDIBSection")
	pSelectObject             = gdi32.NewProc("SelectObject")
	pDeleteObject             = gdi32.NewProc("DeleteObject")
	pBitBlt                   = gdi32.NewProc("BitBlt")
	pCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	pCreatePen                = gdi32.NewProc("CreatePen")
	pGetStockObject           = gdi32.NewProc("GetStockObject")
	pPolygon                  = gdi32.NewProc("Polygon")
	pEllipse                  = gdi32.NewProc("Ellipse")
	pRectangle                = gdi32.NewProc("Rectangle")
	pTextOut                  = gdi32.NewProc("TextOutW")
	pSetTextColor             = gdi32.NewProc("SetTextColor")
	pSetBkColor               = gdi32.NewProc("SetBkColor")
	pSetBkMode                = gdi32.NewProc("SetBkMode")
	pGetTextExtent            = gdi32.NewProc("GetTextExtentPoint32W")
	pCreateFont               = gdi32.NewProc("CreateFontW")
	pRegisterClass            = user32.NewProc("RegisterClassExW")
	pCreateWindow             = user32.NewProc("CreateWindowExW")
	pDefWindowProc            = user32.NewProc("DefWindowProcW")
	pGetMessage               = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessage          = user32.NewProc("DispatchMessageW")
	pPostMessage              = user32.NewProc("PostMessageW")
	pShowWindow               = user32.NewProc("ShowWindow")
	pMoveWindow               = user32.NewProc("MoveWindow")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pSetWindowText            = user32.NewProc("SetWindowTextW")
	pSendMessage              = user32.NewProc("SendMessageW")
	pUpdateLayeredWindow      = user32.NewProc("UpdateLayeredWindow")
	pSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")
	pLoadCursor               = user32.NewProc("LoadCursorW")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pGetModuleHandle          = kernel32.NewProc("GetModuleHandleW")
)

var (
	overlayCB = windows.NewCallback(overlayProc)
	chatCB    = windows.NewCallback(chatProc)
)

func ensureOverlay() { startOverlay() }

func accessibilityTrusted(bool) bool { return true }

func screenSize() (int, int) {
	_, _, w, h := primaryRect()
	return int(w), int(h)
}

func cursorPos() (float64, float64) {
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return float64(p.X), float64(p.Y)
}

func chatSay(role, text string) {
	if text == "" {
		return
	}
	onOverlay(func() { appendChat(role, text) })
}

func setCrosshair(x, y float64, pressed bool, status string) {
	onOverlay(func() {
		ov.x, ov.y, ov.pressed = x, y, pressed
		if status != "" {
			ov.status = status
		}
		redraw()
	})
}

func warpCursor(x, y float64) {
	pSetCursorPos.Call(uintptr(int32(math.Round(x))), uintptr(int32(math.Round(y))))
}

func mouseMove(x, y float64, _ int) {
	nx, ny := absPoint(x, y)
	sendMouse(mouseMoveFlag|mouseAbsolute|mouseVirtualDesk, 0, nx, ny)
}

func mouseButton(x, y float64, button, down, _ int) {
	var flag uint32
	switch button {
	case 2:
		if down != 0 {
			flag = mouseRightDown
		} else {
			flag = mouseRightUp
		}
	case 3:
		if down != 0 {
			flag = mouseMiddleDown
		} else {
			flag = mouseMiddleUp
		}
	default:
		if down != 0 {
			flag = mouseLeftDown
		} else {
			flag = mouseLeftUp
		}
	}
	nx, ny := absPoint(x, y)
	sendMouse(flag|mouseMoveFlag|mouseAbsolute|mouseVirtualDesk, 0, nx, ny)
}

func scrollWheel(dy, dx int) {
	if dy != 0 {
		sendMouse(mouseWheel, uint32(int32(dy)*120), 0, 0)
	}
	if dx != 0 {
		sendMouse(mouseHWheel, uint32(int32(dx)*120), 0, 0)
	}
}

func keyEvent(keycode, down int, _ uint64) {
	vk, ok := macToVK[keycode]
	if !ok {
		return
	}
	flags := uint32(0)
	if extendedVK[vk] {
		flags |= keyEventExtended
	}
	if down == 0 {
		flags |= keyEventKeyUp
	}
	sendKey(vk, 0, flags)
}

func typeUnicode(units []uint16) {
	for _, u := range units {
		sendKey(0, u, keyEventUnicode)
		sendKey(0, u, keyEventUnicode|keyEventKeyUp)
	}
}

func screenshot(hideCrosshair bool) ([]byte, error) {
	if hideCrosshair {
		setOverlayShown(false)
		defer setOverlayShown(true)
		time.Sleep(30 * time.Millisecond)
	}
	img, err := capturePrimary()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sendMouse(flags, data uint32, x, y int32) {
	in := msInput{Dx: x, Dy: y, Data: data, Flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	runtime.KeepAlive(&in)
}

func sendKey(vk, scan uint16, flags uint32) {
	in := kbInput{Type: 1, Vk: vk, Scan: scan, Flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	runtime.KeepAlive(&in)
}

func absPoint(x, y float64) (int32, int32) {
	vx := metric(smXVirtualScreen)
	vy := metric(smYVirtualScreen)
	vw := metric(smCXVirtualScreen)
	vh := metric(smCYVirtualScreen)
	if vw < 2 {
		vw = 2
	}
	if vh < 2 {
		vh = 2
	}
	nx := int32((x - float64(vx)) * 65535 / float64(vw-1))
	ny := int32((y - float64(vy)) * 65535 / float64(vh-1))
	return nx, ny
}

func metric(i uintptr) int32 {
	v, _, _ := pGetSystemMetrics.Call(i)
	return int32(v)
}

func primaryRect() (left, top, width, height int32) {
	mon, _, _ := pMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	r, _, _ := pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.RcMonitor.Right <= info.RcMonitor.Left || info.RcMonitor.Bottom <= info.RcMonitor.Top {
		return 0, 0, metric(0), metric(1)
	}
	return info.RcMonitor.Left, info.RcMonitor.Top,
		info.RcMonitor.Right - info.RcMonitor.Left,
		info.RcMonitor.Bottom - info.RcMonitor.Top
}

func capturePrimary() (*image.RGBA, error) {
	left, top, width, height := primaryRect()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("screen size unavailable")
	}
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer pReleaseDC.Call(0, hdc)
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	if mem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer pDeleteDC.Call(mem)
	hdr := bitmapInfoHeader{Size: 40, Width: width, Height: -height, Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dib, _, _ := pCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer pDeleteObject.Call(dib)
	old, _, _ := pSelectObject.Call(mem, dib)
	defer pSelectObject.Call(mem, old)
	if r, _, _ := pBitBlt.Call(mem, 0, 0, uintptr(width), uintptr(height), hdc, uintptr(left), uintptr(top), srcCopy); r == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}
	raw := unsafe.Slice((*byte)(bits), int(width*height*4))
	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	for i := 0; i < len(raw); i += 4 {
		img.Pix[i] = raw[i+2]
		img.Pix[i+1] = raw[i+1]
		img.Pix[i+2] = raw[i]
		img.Pix[i+3] = 255
	}
	return img, nil
}

// macToVK maps the macOS virtual key codes used by pressKeys to Windows VKs.
var macToVK = map[int]uint16{
	0: 0x41, 1: 0x53, 2: 0x44, 3: 0x46, 4: 0x48, 5: 0x47, 6: 0x5A, 7: 0x58, 8: 0x43, 9: 0x56,
	11: 0x42, 12: 0x51, 13: 0x57, 14: 0x45, 15: 0x52, 16: 0x59, 17: 0x54,
	18: 0x31, 19: 0x32, 20: 0x33, 21: 0x34, 22: 0x36, 23: 0x35, 24: 0xBB, 25: 0x39, 26: 0x37, 27: 0xBD, 28: 0x38, 29: 0x30,
	30: 0xDD, 31: 0x4F, 32: 0x55, 33: 0xDB, 34: 0x49, 35: 0x50, 37: 0x4C, 38: 0x4A, 39: 0xDE, 40: 0x4B, 41: 0xBA, 42: 0xDC,
	43: 0xBC, 44: 0xBF, 45: 0x4E, 46: 0x4D, 47: 0xBE, 50: 0xC0,
	36: 0x0D, 48: 0x09, 49: 0x20, 51: 0x08, 117: 0x2E, 53: 0x1B,
	115: 0x24, 119: 0x23, 116: 0x21, 121: 0x22, 123: 0x25, 124: 0x27, 125: 0x28, 126: 0x26,
	122: 0x70, 120: 0x71, 99: 0x72, 118: 0x73, 96: 0x74, 97: 0x75, 98: 0x76, 100: 0x77, 101: 0x78, 109: 0x79, 103: 0x7A, 111: 0x7B,
	56: 0x10, 59: 0x11, 58: 0x12, 55: 0x5B,
}

var extendedVK = map[uint16]bool{
	0x2E: true, 0x24: true, 0x23: true, 0x21: true, 0x22: true,
	0x25: true, 0x26: true, 0x27: true, 0x28: true, 0x5B: true,
}

type cmdBox struct {
	mu sync.Mutex
	q  []func()
}

func (b *cmdBox) push(fn func()) {
	b.mu.Lock()
	b.q = append(b.q, fn)
	b.mu.Unlock()
}

func (b *cmdBox) drain() {
	for {
		b.mu.Lock()
		if len(b.q) == 0 {
			b.q = nil
			b.mu.Unlock()
			return
		}
		fn := b.q[0]
		b.q = b.q[1:]
		b.mu.Unlock()
		fn()
	}
}

var (
	startOnce    sync.Once
	overlayReady chan struct{}
	overlayCmds  *cmdBox
	overlayHWND  atomic.Uintptr

	chatHWND      uintptr
	chatEdit      uintptr
	chatBtn       uintptr
	chatBrush     uintptr
	chatFont      uintptr
	chatCollapsed bool
	uiScale       = 1.0

	ov struct {
		x, y    float64
		pressed bool
		status  string
		visible bool
	}
)

func startOverlay() {
	startOnce.Do(func() {
		overlayCmds = &cmdBox{}
		overlayReady = make(chan struct{})
		go overlayThread()
	})
	select {
	case <-overlayReady:
	case <-time.After(3 * time.Second):
	}
}

func onOverlay(fn func()) {
	startOverlay()
	hwnd := overlayHWND.Load()
	if hwnd == 0 || overlayCmds == nil {
		return
	}
	done := make(chan struct{})
	overlayCmds.push(func() {
		fn()
		close(done)
	})
	pPostMessage.Call(hwnd, wmRun, 0, 0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func setOverlayShown(shown bool) {
	onOverlay(func() {
		ov.visible = shown
		if shown {
			redraw()
			return
		}
		if hwnd := overlayHWND.Load(); hwnd != 0 {
			pShowWindow.Call(hwnd, swHide)
		}
	})
}

func overlayThread() {
	runtime.LockOSThread()
	initOverlay()
	close(overlayReady)
	if overlayHWND.Load() == 0 {
		return
	}
	for {
		var m winMsg
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func initOverlay() {
	uiScale = loadUIScale()
	inst, _, _ := pGetModuleHandle.Call(0)
	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	chatBrush, _, _ = pCreateSolidBrush.Call(0x00161616)
	face, _ := windows.UTF16PtrFromString("Segoe UI")
	chatFont, _, _ = pCreateFont.Call(
		uintptr(int32(math.Round(-16*uiScale))), 0, 0, 0, 500,
		0, 0, 0, 1, 0, 0, 3, 0, uintptr(unsafe.Pointer(face)),
	)

	registerClass("MaikuComputerOverlay", overlayCB, 0, cursor, inst)
	registerClass("MaikuComputerChat", chatCB, chatBrush, cursor, inst)

	overlay := createWindow(wsExLayered|wsExTransparent|wsExTopmost|wsExToolWindow|wsExNoActivate,
		"MaikuComputerOverlay", "", wsPopup, 0, 0, 1, 1, 0, 0, inst)
	if overlay == 0 {
		return
	}
	overlayHWND.Store(overlay)
	pSetWindowDisplayAffinity.Call(overlay, wdaExcludeFromCapture)

	left, top, sw, _ := primaryRect()
	cw, ch := px(340), px(280)
	chatHWND = createWindow(wsExTopmost|wsExToolWindow|wsExNoActivate,
		"MaikuComputerChat", "", wsPopup|wsClipChildren, left+sw-cw-px(14), top+px(14), cw, ch, 0, 0, inst)
	if chatHWND != 0 {
		pSetWindowDisplayAffinity.Call(chatHWND, wdaExcludeFromCapture)
		chatBtn = createWindow(0, "BUTTON", "Agent  ▾", wsChild|wsVisible, px(8), px(6), cw-px(16), px(24), chatHWND, 1, inst)
		chatEdit = createWindow(0, "EDIT", "", wsChild|wsVisible|wsVScroll|0x0004|0x0800|0x0040,
			px(8), px(36), cw-px(16), ch-px(44), chatHWND, 0, inst)
		if chatFont != 0 {
			if chatBtn != 0 {
				pSendMessage.Call(chatBtn, wmSetFont, chatFont, 1)
			}
			if chatEdit != 0 {
				pSendMessage.Call(chatEdit, wmSetFont, chatFont, 1)
			}
		}
		pSetWindowPos.Call(chatHWND, ^uintptr(0), 0, 0, 0, 0, swpNoActivate|swpShowWindow|0x0001|0x0002)
		layoutChat(cw, ch)
	}

	w, h := screenSize()
	ov.x, ov.y = float64(w)/2, float64(h)/2
	ov.visible = true
	redraw()
}

func loadUIScale() float64 {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return 1
	}
	defer pReleaseDC.Call(0, hdc)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, logPixelsX)
	if dpi < 96 {
		return 1
	}
	return float64(dpi) / 96
}

func px(n int32) int32 { return int32(math.Round(float64(n) * uiScale)) }

func registerClass(name string, proc, brush, cursor, inst uintptr) {
	cls, _ := windows.UTF16PtrFromString(name)
	wc := wndClassEx{
		Size: 80, WndProc: proc, Instance: inst, Cursor: cursor, Background: brush, Class: cls,
	}
	pRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(&wc)
	runtime.KeepAlive(cls)
}

func createWindow(ex uintptr, class, title string, style uintptr, x, y, w, h int32, parent, id, inst uintptr) uintptr {
	cls, _ := windows.UTF16PtrFromString(class)
	name, _ := windows.UTF16PtrFromString(title)
	hwnd, _, _ := pCreateWindow.Call(
		ex,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(name)),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, inst, 0,
	)
	return hwnd
}

func overlayProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == wmRun {
		if overlayCmds != nil {
			overlayCmds.drain()
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func chatProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCommand:
		if wparam&0xffff == 1 && (wparam>>16)&0xffff == 0 {
			toggleChat()
			return 0
		}
	case wmSize:
		layoutChat(int32(lparam&0xffff), int32((lparam>>16)&0xffff))
		return 0
	case wmCtlColorEdit, wmCtlColorStatic, wmCtlColorBtn:
		pSetTextColor.Call(wparam, 0x00E8E8E8)
		pSetBkColor.Call(wparam, 0x00161616)
		return chatBrush
	case wmClose:
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func toggleChat() {
	chatCollapsed = !chatCollapsed
	var r rect
	pGetWindowRect.Call(chatHWND, uintptr(unsafe.Pointer(&r)))
	h := px(280)
	title := "Agent  ▾"
	if chatCollapsed {
		h = px(36)
		title = "Agent  ▸"
	}
	pSetWindowPos.Call(chatHWND, ^uintptr(0), uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(h), swpNoActivate)
	if chatEdit != 0 {
		show := uintptr(swShowNA)
		if chatCollapsed {
			show = swHide
		}
		pShowWindow.Call(chatEdit, show)
	}
	setTitle(chatBtn, title)
}

func layoutChat(w, h int32) {
	if chatBtn == 0 || w <= 0 || h <= 0 {
		return
	}
	pMoveWindow.Call(chatBtn, uintptr(px(8)), uintptr(px(6)), uintptr(w-px(16)), uintptr(px(24)), 1)
	if chatEdit != 0 && !chatCollapsed {
		pMoveWindow.Call(chatEdit, uintptr(px(8)), uintptr(px(36)), uintptr(w-px(16)), uintptr(h-px(44)), 1)
	}
}

func appendChat(role, text string) {
	if chatEdit == 0 {
		return
	}
	if n, _, _ := pSendMessage.Call(chatEdit, wmGetTextLength, 0, 0); n > 24000 {
		setTitle(chatEdit, "")
	}
	line := chatLine(role, text)
	u, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return
	}
	end, _, _ := pSendMessage.Call(chatEdit, wmGetTextLength, 0, 0)
	pSendMessage.Call(chatEdit, emSetSel, end, end)
	pSendMessage.Call(chatEdit, emReplaceSel, 0, uintptr(unsafe.Pointer(u)))
	pSendMessage.Call(chatEdit, emScrollCaret, 0, 0)
	runtime.KeepAlive(u)
}

func chatLine(role, text string) string {
	switch role {
	case "act":
		return "→ " + text + "\r\n\r\n"
	case "done":
		return "✓ " + text + "\r\n\r\n"
	case "task":
		return "Task  " + text + "\r\n\r\n"
	default:
		return text + "\r\n\r\n"
	}
}

func setTitle(hwnd uintptr, title string) {
	if hwnd == 0 {
		return
	}
	u, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	pSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(u)))
	runtime.KeepAlive(u)
}

func redraw() {
	hwnd := overlayHWND.Load()
	if hwnd == 0 || !ov.visible {
		return
	}
	scale := 0.78 * uiScale
	if ov.pressed {
		scale = 0.70 * uiScale
	}
	canvasW, canvasH := px(340), px(72)
	left, top, sw, sh := primaryRect()

	hdcScreen, _, _ := pGetDC.Call(0)
	if hdcScreen == 0 {
		return
	}
	defer pReleaseDC.Call(0, hdcScreen)
	mem, _, _ := pCreateCompatibleDC.Call(hdcScreen)
	if mem == 0 {
		return
	}
	defer pDeleteDC.Call(mem)
	hdr := bitmapInfoHeader{Size: 40, Width: canvasW, Height: -canvasH, Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dib, _, _ := pCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return
	}
	defer pDeleteObject.Call(dib)
	pix := unsafe.Slice((*byte)(bits), int(canvasW*canvasH*4))
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = 255, 0, 255, 0
	}
	oldBmp, _, _ := pSelectObject.Call(mem, dib)
	defer pSelectObject.Call(mem, oldBmp)
	var drop []uintptr
	defer func() {
		for _, h := range drop {
			if h != 0 {
				pDeleteObject.Call(h)
			}
		}
	}()

	var oldFont, oldBrush, oldPen uintptr
	if chatFont != 0 {
		oldFont, _, _ = pSelectObject.Call(mem, chatFont)
		defer pSelectObject.Call(mem, oldFont)
	}
	pSetBkMode.Call(mem, 1)
	pSetTextColor.Call(mem, 0x00FFFFFF)

	tipX := int32(px(4))
	tipY := int32(px(4))
	winX := int32(math.Round(ov.x)) - tipX
	winY := int32(math.Round(ov.y)) - tipY
	right, bottom := left+sw, top+sh
	if winX < left {
		tipX += left - winX
		winX = left
	}
	if winY < top {
		tipY += top - winY
		winY = top
	}
	if winX+canvasW > right {
		shift := winX + canvasW - right
		winX -= shift
		tipX += shift
	}
	if winY+canvasH > bottom {
		shift := winY + canvasH - bottom
		winY -= shift
		tipY += shift
	}

	if ov.status != "" {
		u, err := windows.UTF16FromString(ov.status)
		if err == nil && len(u) > 1 {
			var ext winSize
			pGetTextExtent.Call(mem, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&ext)))
			boxW, boxH := ext.CX+px(14), ext.CY+px(8)
			labelX, labelY := tipX+px(18), tipY+px(8)
			if labelX+boxW > canvasW-px(4) {
				labelX = tipX - px(8) - boxW
			}
			if labelX < px(2) {
				labelX = px(2)
			}
			fillRect(mem, labelX, labelY, labelX+boxW, labelY+boxH, 0x00202020, &oldBrush, &oldPen, &drop)
			pTextOut.Call(mem, uintptr(labelX+px(7)), uintptr(labelY+px(4)), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
		}
	}

	if ov.pressed {
		c := colorGreen
		fillEllipse(mem, tipX-px(8), tipY-px(8), tipX+px(8), tipY+px(8), c, &oldBrush, &oldPen, &drop)
	}
	pts := arrowPoints(tipX, tipY, scale)
	shadow := arrowPoints(tipX+px(1), tipY+px(1), scale)
	stroke(mem, shadow, 0, 0x00000000, &oldBrush, &oldPen, &drop)
	stroke(mem, pts, int32(math.Max(2, math.Round(3*uiScale))), 0x00FFFFFF, &oldBrush, &oldPen, &drop)
	main := colorIndigo
	if ov.pressed {
		main = colorGreen
	}
	stroke(mem, pts, int32(math.Max(1, math.Round(uiScale))), main, &oldBrush, &oldPen, &drop)

	for i := 0; i < len(pix); i += 4 {
		if pix[i] == 255 && pix[i+1] == 0 && pix[i+2] == 255 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = 0, 0, 0, 0
			continue
		}
		pix[i+3] = 255
	}
	if oldFont != 0 {
		pSelectObject.Call(mem, oldFont)
	}
	if oldBrush != 0 {
		pSelectObject.Call(mem, oldBrush)
	}
	if oldPen != 0 {
		pSelectObject.Call(mem, oldPen)
	}

	dst := point{X: winX, Y: winY}
	sz := winSize{CX: canvasW, CY: canvasH}
	src := point{}
	blend := blendFunc{Alpha: 255, Format: 1}
	pUpdateLayeredWindow.Call(hwnd, 0, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&sz)), mem, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	runtime.KeepAlive(&dst)
	runtime.KeepAlive(&sz)
	runtime.KeepAlive(&src)
	runtime.KeepAlive(&blend)
}

const (
	colorIndigo uint32 = 0x00E65C5E
	colorGreen  uint32 = 0x0058D130
)

func arrowPoints(tipX, tipY int32, scale float64) []point {
	raw := [][2]float64{{0, 0}, {0, 16}, {4.2, 12.3}, {7, 18.6}, {9.6, 17.5}, {6.9, 11.4}, {12.2, 11.4}}
	pts := make([]point, len(raw))
	for i, p := range raw {
		pts[i] = point{
			X: tipX + int32(math.Round(p[0]*scale)),
			Y: tipY + int32(math.Round(p[1]*scale)),
		}
	}
	return pts
}

func selectPair(hdc, brush, pen uintptr, oldBrush, oldPen *uintptr) {
	if *oldBrush == 0 {
		*oldBrush, _, _ = pSelectObject.Call(hdc, brush)
	} else {
		pSelectObject.Call(hdc, brush)
	}
	if *oldPen == 0 {
		*oldPen, _, _ = pSelectObject.Call(hdc, pen)
	} else {
		pSelectObject.Call(hdc, pen)
	}
}

func stroke(hdc uintptr, pts []point, width int32, color uint32, oldBrush, oldPen *uintptr, drop *[]uintptr) {
	if len(pts) == 0 {
		return
	}
	brush, _, _ := pCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := pCreatePen.Call(0, uintptr(width), uintptr(color))
	*drop = append(*drop, brush, pen)
	selectPair(hdc, brush, pen, oldBrush, oldPen)
	pPolygon.Call(hdc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	runtime.KeepAlive(pts)
}

func fillRect(hdc uintptr, l, t, r, b int32, color uint32, oldBrush, oldPen *uintptr, drop *[]uintptr) {
	brush, _, _ := pCreateSolidBrush.Call(uintptr(color))
	*drop = append(*drop, brush)
	stock, _, _ := pGetStockObject.Call(nullPen)
	selectPair(hdc, brush, stock, oldBrush, oldPen)
	pRectangle.Call(hdc, uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}

func fillEllipse(hdc uintptr, l, t, r, b int32, color uint32, oldBrush, oldPen *uintptr, drop *[]uintptr) {
	brush, _, _ := pCreateSolidBrush.Call(uintptr(color))
	*drop = append(*drop, brush)
	stock, _, _ := pGetStockObject.Call(nullPen)
	selectPair(hdc, brush, stock, oldBrush, oldPen)
	pEllipse.Call(hdc, uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}
