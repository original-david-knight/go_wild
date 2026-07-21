package main

const (
	sandboxImageName = "gowild-agent"
	sandboxImageTag  = "latest"
	containerPrefix  = "gowild-agent-"
	volumePrefix     = "gowild-agent-"
	volumeSuffix     = "-data"
	flagsLabel       = "gowild.extra-flags"
)

// containerName returns the Docker container name for an agent.
func containerName(agentID string) string {
	return containerPrefix + agentID
}

// volumeName returns the Docker volume name for an agent.
func volumeName(agentID string) string {
	return volumePrefix + agentID + volumeSuffix
}

// imageName returns the full image name with tag.
func imageName() string {
	return sandboxImageName + ":" + sandboxImageTag
}

// SandboxConfig holds configuration for running an agent in a sandbox.
type SandboxConfig struct {
	AgentID     string
	EnvVars     map[string]string // Environment variables to pass to container
	ExtraFlags  []string          // Additional flags to pass to the agent binary
	ExtraMounts []string          // Additional -v mounts (host:container format)
	Rebuild     bool              // Force rebuild of the image
	AttachStdin bool              // Whether to attach stdin (interactive mode)
	Background  bool              // Run in background (detached)
	NetworkMode string            // Docker network mode (default: bridge)
	MemoryLimit string            // Memory limit (default: 2g)
	CPULimit    string            // CPU limit (default: 2)
}

// SandboxInfo contains information about an agent sandbox.
type SandboxInfo struct {
	AgentID   string
	Container string
	Status    string
}
