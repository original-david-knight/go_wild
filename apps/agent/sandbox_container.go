package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// ContainerStatus returns the status of an agent's container.
// Returns: "running", "exited", "created", or "" if not found.
func ContainerStatus(ctx context.Context, agentID string) string {
	name := containerName(agentID)
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", name)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "" // Container doesn't exist
	}
	return strings.TrimSpace(stdout.String())
}

// containerFlags returns the extra-flags label from an existing container.
func containerFlags(ctx context.Context, agentID string) string {
	name := containerName(agentID)
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{index .Config.Labels \""+flagsLabel+"\"}}", name)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// StartSandbox starts or creates an agent's sandbox container.
func StartSandbox(ctx context.Context, config SandboxConfig) error {
	name := containerName(config.AgentID)

	// Check if running via "go run" - sandbox requires a built binary
	exePath, err := os.Executable()
	if err == nil && strings.Contains(exePath, "go-build") {
		return fmt.Errorf("sandbox mode requires a built binary. Run 'go build' first, then './agent'")
	}

	// If rebuilding, remove the old container so we use the new image
	if config.Rebuild {
		if status := ContainerStatus(ctx, config.AgentID); status != "" {
			fmt.Println(color.CyanString("Removing old container for rebuild..."))
			if err := RemoveSandbox(ctx, config.AgentID); err != nil {
				return fmt.Errorf("failed to remove old container: %w", err)
			}
		}
	}

	// Check container status
	status := ContainerStatus(ctx, config.AgentID)

	switch status {
	case "running":
		if config.Background {
			fmt.Println(color.GreenString("Container %s is already running", name))
			return nil
		}
		// Attach to running container
		return attachToContainer(ctx, name, config.AttachStdin)

	case "exited", "created":
		// Check if flags changed since the container was created.
		currentFlags := strings.Join(config.ExtraFlags, " ")
		savedFlags := containerFlags(ctx, config.AgentID)
		if currentFlags != savedFlags {
			// Flags changed — recreate so new flags are honored.
			// Agent data lives on the volume, so nothing is lost.
			if err := RemoveSandbox(ctx, config.AgentID); err != nil {
				return fmt.Errorf("failed to remove old container: %w", err)
			}
			return createAndRunContainer(ctx, config)
		}

		fmt.Println(color.CyanString("Starting container %s...", name))

		if config.Background {
			cmd := exec.CommandContext(ctx, "docker", "start", name)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
			fmt.Println(color.GreenString("Container %s started in background", name))
			return nil
		}

		// Interactive mode: use docker start -ai to attach immediately
		startArgs := []string{"start", "-a"}
		if config.AttachStdin {
			startArgs = append(startArgs, "-i")
		}
		startArgs = append(startArgs, name)

		cmd := exec.CommandContext(ctx, "docker", startArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if config.AttachStdin {
			cmd.Stdin = os.Stdin
		}
		return cmd.Run()

	default:
		// Create new container
		return createAndRunContainer(ctx, config)
	}
}

// createAndRunContainer creates a new container and runs it.
func createAndRunContainer(ctx context.Context, config SandboxConfig) error {
	name := containerName(config.AgentID)
	volName := volumeName(config.AgentID)

	// Ensure volume exists
	if err := EnsureVolume(ctx, config.AgentID); err != nil {
		return err
	}

	// Get the current executable path to mount into container
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	// Resolve symlinks to get the real path
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Build docker run arguments
	args := []string{"run"}

	// Container name
	args = append(args, "--name", name)

	// Interactive mode
	if config.AttachStdin && !config.Background {
		args = append(args, "-it")
	} else if config.Background {
		args = append(args, "-d")
	} else {
		args = append(args, "-t")
	}

	// Mount volume for persistent data
	args = append(args, "-v", volName+":/data")

	// Mount the local binary into the container (for development)
	// This allows code changes without rebuilding the Docker image
	args = append(args, "-v", exePath+":/usr/local/bin/agent:ro")

	// Mount home directory read-only so /file command can access host files
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		args = append(args, "-v", homeDir+":"+homeDir+":ro")
	}

	// Extra mounts (e.g., host logs directory)
	for _, mount := range config.ExtraMounts {
		args = append(args, "-v", mount)
	}

	// Mount Docker socket for nested containers (Python sandbox, browser)
	args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")

	// Add agent user to docker group for socket access
	args = append(args, "--group-add", getDockerGID())

	// Network mode (default: bridge for internet access)
	networkMode := config.NetworkMode
	if networkMode == "" {
		networkMode = "bridge"
	}
	args = append(args, "--network", networkMode)

	// Add host.docker.internal mapping for Linux (allows container to access host services)
	// This is needed to connect to PostgreSQL running on the host
	args = append(args, "--add-host=host.docker.internal:host-gateway")

	// Resource limits
	memLimit := config.MemoryLimit
	if memLimit == "" {
		memLimit = "2g"
	}
	args = append(args, "--memory", memLimit)

	cpuLimit := config.CPULimit
	if cpuLimit == "" {
		cpuLimit = "2"
	}
	args = append(args, "--cpus", cpuLimit)

	// Security: prevent privilege escalation but allow docker access
	args = append(args, "--security-opt=no-new-privileges")

	// Store flags as a label so we can detect changes on restart
	args = append(args, "--label", flagsLabel+"="+strings.Join(config.ExtraFlags, " "))

	// Environment variables
	for key, value := range config.EnvVars {
		args = append(args, "-e", key+"="+value)
	}

	// Image
	args = append(args, imageName())

	// Agent flags
	args = append(args, "-agent", config.AgentID)
	args = append(args, config.ExtraFlags...)

	fmt.Println(color.CyanString("Creating container %s...", name))

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if config.AttachStdin && !config.Background {
		cmd.Stdin = os.Stdin
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	if config.Background {
		fmt.Println(color.GreenString("Container %s started in background", name))
	}

	return nil
}

// attachToContainer attaches to a running container.
func attachToContainer(ctx context.Context, name string, attachStdin bool) error {
	fmt.Println(color.CyanString("Attaching to container %s...", name))

	args := []string{"attach", name}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if attachStdin {
		cmd.Stdin = os.Stdin
	}

	return cmd.Run()
}
