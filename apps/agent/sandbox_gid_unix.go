//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// getDockerGID returns the GID of the docker group on the host.
func getDockerGID() string {
	info, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		return "docker"
	}

	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d", stat.Gid)
	}

	return "docker"
}
