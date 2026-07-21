package main

import (
	"os"
	"syscall"
)

func daemonSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func isHotkeySignal(os.Signal) bool {
	return false
}

func isShutdownSignal(signal os.Signal) bool {
	return signal == os.Interrupt || signal == syscall.SIGTERM
}

func daemonSignalDescription() string {
	return "listening for control socket commands (SIGUSR1 is unavailable on Windows)"
}
