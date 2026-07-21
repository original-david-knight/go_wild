package dockermgr

import (
	"strconv"
	"strings"
)

// ContainerName returns the Docker container name for an agent.
func ContainerName(agentID string) string {
	return containerPrefix + agentID
}

// VolumeName returns the Docker volume name for an agent.
func VolumeName(agentID string) string {
	return volumePrefix + agentID + volumeSuffix
}

// ParseMemoryBytes parses a memory limit string (e.g. "2g", "512m") to bytes.
func ParseMemoryBytes(limit string) int64 {
	if limit == "" {
		limit = "2g"
	}

	limit = strings.TrimSpace(strings.ToLower(limit))

	multiplier := int64(1)
	if strings.HasSuffix(limit, "g") {
		multiplier = 1024 * 1024 * 1024
		limit = strings.TrimSuffix(limit, "g")
	} else if strings.HasSuffix(limit, "m") {
		multiplier = 1024 * 1024
		limit = strings.TrimSuffix(limit, "m")
	} else if strings.HasSuffix(limit, "k") {
		multiplier = 1024
		limit = strings.TrimSuffix(limit, "k")
	}

	val, err := strconv.ParseInt(limit, 10, 64)
	if err != nil {
		return 2 * 1024 * 1024 * 1024 // Default 2GB
	}

	return val * multiplier
}

// ParseCPUs parses a CPU limit string (e.g. "2", "0.5") to nanoseconds.
func ParseCPUs(limit string) int64 {
	if limit == "" {
		limit = "2"
	}

	limit = strings.TrimSpace(limit)
	val, err := strconv.ParseFloat(limit, 64)
	if err != nil {
		return 2 * 1e9 // Default 2 CPUs
	}

	return int64(val * 1e9)
}
