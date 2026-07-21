package main

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

var (
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmHotkey = 0x0312
	wmQuit   = 0x0012

	assistHotkeyID = 1
)

type hotkeySpec struct {
	Modifiers uint32
	VirtualKey uint32
}

var namedVirtualKeys = map[string]uint32{
	"space":       0x20,
	"enter":       0x0D,
	"return":      0x0D,
	"tab":         0x09,
	"escape":      0x1B,
	"esc":         0x1B,
	"backspace":   0x08,
	"insert":      0x2D,
	"delete":      0x2E,
	"del":         0x2E,
	"home":        0x24,
	"end":         0x23,
	"pageup":      0x21,
	"pagedown":    0x22,
	"up":          0x26,
	"down":        0x28,
	"left":        0x25,
	"right":       0x27,
	"pause":       0x13,
	"printscreen": 0x2C,
}

// parseHotkey parses combos like "ctrl+alt+a", "ctrl+shift+f9", or "win+space".
func parseHotkey(raw string) (hotkeySpec, error) {
	var spec hotkeySpec
	haveKey := false
	for _, token := range strings.Split(strings.ToLower(strings.TrimSpace(raw)), "+") {
		token = strings.TrimSpace(token)
		switch token {
		case "":
			return hotkeySpec{}, fmt.Errorf("empty token in hotkey %q", raw)
		case "ctrl", "control":
			spec.Modifiers |= modControl
		case "alt":
			spec.Modifiers |= modAlt
		case "shift":
			spec.Modifiers |= modShift
		case "win", "super", "meta":
			spec.Modifiers |= modWin
		default:
			if haveKey {
				return hotkeySpec{}, fmt.Errorf("hotkey %q has more than one non-modifier key", raw)
			}
			vk, err := parseVirtualKey(token)
			if err != nil {
				return hotkeySpec{}, fmt.Errorf("hotkey %q: %w", raw, err)
			}
			spec.VirtualKey = vk
			haveKey = true
		}
	}
	if !haveKey {
		return hotkeySpec{}, fmt.Errorf("hotkey %q has no non-modifier key", raw)
	}
	isFunctionKey := spec.VirtualKey >= 0x70 && spec.VirtualKey <= 0x87
	if spec.Modifiers == 0 && !isFunctionKey {
		return hotkeySpec{}, fmt.Errorf("hotkey %q needs at least one modifier (ctrl, alt, shift, win)", raw)
	}
	return spec, nil
}

func parseVirtualKey(token string) (uint32, error) {
	if vk, ok := namedVirtualKeys[token]; ok {
		return vk, nil
	}
	if len(token) == 1 {
		c := token[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint32(c - 'a' + 'A'), nil
		case c >= '0' && c <= '9':
			return uint32(c), nil
		}
	}
	if strings.HasPrefix(token, "f") && len(token) > 1 {
		n := 0
		for _, r := range token[1:] {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), nil
		}
	}
	return 0, fmt.Errorf("unknown key %q", token)
}

type winPoint struct {
	X, Y int32
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
}

// StartHotkeyListener registers a system-wide hotkey and delivers an event
// each time it fires. It returns a nil channel when no hotkey is configured.
func StartHotkeyListener(cfg Config, logger DebugLogger) (<-chan struct{}, func(), error) {
	combo := strings.TrimSpace(cfg.Hotkey)
	if combo == "" {
		return nil, func() {}, nil
	}
	spec, err := parseHotkey(combo)
	if err != nil {
		return nil, nil, err
	}

	events := make(chan struct{}, 1)
	ready := make(chan error, 1)
	done := make(chan struct{})
	var threadID uintptr

	go func() {
		defer close(done)
		// RegisterHotKey binds the hotkey to this thread's message queue, so
		// registration and the message loop must share one OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		threadID, _, _ = procGetCurrentThreadId.Call()
		if r, _, callErr := procRegisterHotKey.Call(0, assistHotkeyID, uintptr(spec.Modifiers|modNoRepeat), uintptr(spec.VirtualKey)); r == 0 {
			ready <- fmt.Errorf("register hotkey %q: %v", combo, callErr)
			return
		}
		defer procUnregisterHotKey.Call(0, assistHotkeyID)
		ready <- nil

		var msg winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				return // WM_QUIT or queue error
			}
			if msg.Message == wmHotkey && msg.WParam == assistHotkeyID {
				logger.Printf("global hotkey fired: %s", combo)
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()

	if err := <-ready; err != nil {
		<-done
		return nil, nil, err
	}
	stop := func() {
		procPostThreadMessageW.Call(threadID, wmQuit, 0, 0)
		<-done
	}
	return events, stop, nil
}
