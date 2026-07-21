//go:build !windows

package main

import (
	"os"
	"syscall"
)

func daemonSignals() []os.Signal {
	return []os.Signal{syscall.SIGUSR1, syscall.SIGINT, syscall.SIGTERM}
}

func isHotkeySignal(signal os.Signal) bool {
	return signal == syscall.SIGUSR1
}

func isShutdownSignal(signal os.Signal) bool {
	return signal == syscall.SIGINT || signal == syscall.SIGTERM
}

func daemonSignalDescription() string {
	return "listening for SIGUSR1"
}
