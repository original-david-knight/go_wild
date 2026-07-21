//go:build !windows

package dockermgr

import (
	"os"
	"strconv"
	"syscall"
)

func getDockerGID() string {
	stat, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		return "docker"
	}

	if sysStat, ok := stat.Sys().(*syscall.Stat_t); ok {
		return strconv.FormatUint(uint64(sysStat.Gid), 10)
	}

	return "docker"
}
