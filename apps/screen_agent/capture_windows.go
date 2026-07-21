package main

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsCaptureService captures the screen natively through GDI, using the
// foreground window and monitor geometry from user32/dwmapi. No external
// tools are required.
type WindowsCaptureService struct {
	Config Config
	Getenv func(string) string
	Logger DebugLogger
	Now    func() time.Time
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetClassNameW                 = user32.NewProc("GetClassNameW")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procIsIconic                      = user32.NewProc("IsIconic")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwareness        = shcore.NewProc("SetProcessDpiAwareness")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procGdiFlush           = gdi32.NewProc("GdiFlush")

	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	monitorDefaultToPrimary = 1
	monitorDefaultToNearest = 2

	dwmwaExtendedFrameBounds = 9

	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	captureBlt   = 0x40000000
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

func (r winRect) geometry() geometry {
	return geometry{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

type winMonitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

var dpiAwarenessOnce sync.Once

// ensureDPIAwareness opts the process into per-monitor DPI awareness so
// window and monitor geometry come back in physical pixels that match the
// coordinates BitBlt reads from the screen DC.
func ensureDPIAwareness() {
	dpiAwarenessOnce.Do(func() {
		if procSetProcessDpiAwarenessContext.Find() == nil {
			perMonitorAwareV2 := ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
			if r, _, _ := procSetProcessDpiAwarenessContext.Call(perMonitorAwareV2); r != 0 {
				return
			}
		}
		if procSetProcessDpiAwareness.Find() == nil {
			const processPerMonitorDPIAware = 2
			if r, _, _ := procSetProcessDpiAwareness.Call(processPerMonitorDPIAware); r == 0 {
				return
			}
		}
		_, _, _ = procSetProcessDPIAware.Call()
	})
}

func (s WindowsCaptureService) Capture(ctx context.Context, mode CaptureMode, opts CaptureOptions) (CaptureResult, error) {
	ensureDPIAwareness()
	now := s.Now
	if now == nil {
		now = time.Now
	}
	if err := ctx.Err(); err != nil {
		return CaptureResult{}, err
	}

	active, activeErr := foregroundWindowInfo()
	if activeErr != nil {
		s.Logger.Printf("active window metadata unavailable: %v", activeErr)
	}

	var rect winRect
	switch mode {
	case CaptureModeWindow:
		if activeErr != nil {
			return CaptureResult{}, fmt.Errorf("active window geometry unavailable: %w", activeErr)
		}
		if !active.Geometry.valid() {
			return CaptureResult{}, fmt.Errorf("active window geometry unavailable: active window has invalid size")
		}
		rect = rectFromGeometry(active.Geometry)
	case CaptureModeMonitor:
		r, err := focusedMonitorRect()
		if err != nil {
			return CaptureResult{}, fmt.Errorf("focused monitor geometry unavailable: %w", err)
		}
		rect = r
	case CaptureModeFull:
		r, err := virtualScreenRect()
		if err != nil {
			return CaptureResult{}, err
		}
		rect = r
	case CaptureModeRegion:
		return CaptureResult{}, fmt.Errorf("region capture is not supported on Windows")
	default:
		return CaptureResult{}, fmt.Errorf("unsupported capture mode %q", mode)
	}

	if mode != CaptureModeFull {
		bounds, err := virtualScreenRect()
		if err != nil {
			return CaptureResult{}, err
		}
		rect = intersectRect(rect, bounds)
	}
	if rect.Right-rect.Left <= 0 || rect.Bottom-rect.Top <= 0 {
		return CaptureResult{}, fmt.Errorf("capture geometry %v is outside the visible screen", rect.geometry())
	}

	path, cleanup, err := resolveCapturePath(s.Config, s.Getenv, opts, now())
	if err != nil {
		return CaptureResult{}, err
	}
	s.Logger.Printf("capture rect: %s -> %s", rect.geometry(), path)
	img, err := captureScreenRect(rect)
	if err != nil {
		_ = cleanup()
		return CaptureResult{}, fmt.Errorf("capture screenshot: %w", err)
	}
	if err := writePNG(path, img); err != nil {
		_ = cleanup()
		return CaptureResult{}, err
	}

	return CaptureResult{
		ImagePath:   path,
		Mode:        mode,
		Width:       img.Bounds().Dx(),
		Height:      img.Bounds().Dy(),
		ActiveTitle: active.Title,
		ActiveClass: active.Class,
		CapturedAt:  now(),
		Cleanup:     cleanup,
	}, nil
}

func rectFromGeometry(g geometry) winRect {
	return winRect{Left: int32(g.X), Top: int32(g.Y), Right: int32(g.X + g.W), Bottom: int32(g.Y + g.H)}
}

func intersectRect(a, b winRect) winRect {
	r := a
	if b.Left > r.Left {
		r.Left = b.Left
	}
	if b.Top > r.Top {
		r.Top = b.Top
	}
	if b.Right < r.Right {
		r.Right = b.Right
	}
	if b.Bottom < r.Bottom {
		r.Bottom = b.Bottom
	}
	if r.Right < r.Left {
		r.Right = r.Left
	}
	if r.Bottom < r.Top {
		r.Bottom = r.Top
	}
	return r
}

func foregroundWindowInfo() (activeWindow, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return activeWindow{}, fmt.Errorf("no foreground window")
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return activeWindow{}, fmt.Errorf("foreground window is minimized")
	}

	var titleBuf [512]uint16
	titleLen, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&titleBuf[0])), uintptr(len(titleBuf)))
	var classBuf [256]uint16
	classLen, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&classBuf[0])), uintptr(len(classBuf)))

	rect, err := windowRect(hwnd)
	if err != nil {
		return activeWindow{}, err
	}
	return activeWindow{
		Title:    windows.UTF16ToString(titleBuf[:titleLen]),
		Class:    windows.UTF16ToString(classBuf[:classLen]),
		Geometry: rect.geometry(),
	}, nil
}

func windowRect(hwnd uintptr) (winRect, error) {
	var rect winRect
	// Extended frame bounds exclude the invisible resize borders and drop
	// shadow that GetWindowRect includes on composited desktops.
	hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&rect)), unsafe.Sizeof(rect))
	if hr == 0 {
		return rect, nil
	}
	if r, _, callErr := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); r == 0 {
		return winRect{}, fmt.Errorf("GetWindowRect: %v", callErr)
	}
	return rect, nil
}

func focusedMonitorRect() (winRect, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	var monitor uintptr
	if hwnd != 0 {
		monitor, _, _ = procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	}
	if monitor == 0 {
		monitor, _, _ = procMonitorFromWindow.Call(0, monitorDefaultToPrimary)
	}
	if monitor == 0 {
		return winRect{}, fmt.Errorf("no display monitor found")
	}
	var info winMonitorInfo
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, callErr := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); r == 0 {
		return winRect{}, fmt.Errorf("GetMonitorInfo: %v", callErr)
	}
	return info.Monitor, nil
}

func virtualScreenRect() (winRect, error) {
	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	w, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if int32(w) <= 0 || int32(h) <= 0 {
		return winRect{}, fmt.Errorf("virtual screen has invalid size %dx%d", int32(w), int32(h))
	}
	return winRect{
		Left:   int32(x),
		Top:    int32(y),
		Right:  int32(x) + int32(w),
		Bottom: int32(y) + int32(h),
	}, nil
}

func captureScreenRect(rect winRect) (*image.RGBA, error) {
	width := int(rect.Right - rect.Left)
	height := int(rect.Bottom - rect.Top)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC failed for the screen")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	header := bitmapInfoHeader{
		Width:       int32(width),
		Height:      -int32(height), // top-down rows
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
	header.Size = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	bitmap, _, callErr := procCreateDIBSection.Call(screenDC, uintptr(unsafe.Pointer(&header)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection: %v", callErr)
	}
	defer procDeleteObject.Call(bitmap)

	previous, _, _ := procSelectObject.Call(memDC, bitmap)
	if previous == 0 {
		return nil, fmt.Errorf("SelectObject failed")
	}
	defer procSelectObject.Call(memDC, previous)

	if r, _, callErr := procBitBlt.Call(
		memDC, 0, 0, uintptr(width), uintptr(height),
		screenDC, uintptr(int(rect.Left)), uintptr(int(rect.Top)),
		srcCopy|captureBlt,
	); r == 0 {
		return nil, fmt.Errorf("BitBlt: %v", callErr)
	}
	procGdiFlush.Call()

	source := unsafe.Slice((*byte)(bits), width*height*4)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i+3 < len(source); i += 4 {
		img.Pix[i] = source[i+2] // BGRA -> RGBA
		img.Pix[i+1] = source[i+1]
		img.Pix[i+2] = source[i]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create screenshot file: %w", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode screenshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close screenshot file: %w", err)
	}
	return nil
}
