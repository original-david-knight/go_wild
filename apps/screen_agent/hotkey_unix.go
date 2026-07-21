//go:build !windows

package main

// StartHotkeyListener is a no-op outside Windows: key combos are bound in the
// compositor and delivered as SIGUSR1 or control-socket commands instead.
func StartHotkeyListener(cfg Config, logger DebugLogger) (<-chan struct{}, func(), error) {
	return nil, func() {}, nil
}
