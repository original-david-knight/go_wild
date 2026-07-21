package dockermgr

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

// ContainerCreateConfig holds the pre-computed configuration for creating an agent container.
// The caller is responsible for building the Cmd, Env, and Labels from agent-specific
// configuration (broker auth, runtime flags, etc.).
type ContainerCreateConfig struct {
	AgentID     string
	Cmd         []string
	Env         []string
	Labels      map[string]string
	MemoryBytes int64
	NanoCPUs    int64
	ExtraMounts []mount.Mount
}

// CreateContainer creates a Docker container for the agent.
// Auto-builds the image only if it doesn't exist. Use BuildImage to force a rebuild.
func (dm *DockerManager) CreateContainer(ctx context.Context, cfg ContainerCreateConfig) error {
	// Only build if image doesn't exist at all
	if !dm.ImageExists(ctx) {
		if err := dm.EnsureImage(ctx); err != nil {
			return fmt.Errorf("failed to build image: %w", err)
		}
	}

	name := ContainerName(cfg.AgentID)
	volName := VolumeName(cfg.AgentID)

	// Ensure volume exists
	if err := dm.EnsureVolume(ctx, cfg.AgentID); err != nil {
		return err
	}

	ownerUID, ownerGID := os.Getuid(), os.Getgid()
	// Keep volume ownership aligned with the manager user so broker-side tools
	// (which run as the manager user) can access agent-created files safely.
	if err := dm.ensureVolumeOwnership(ctx, cfg.AgentID, ownerUID, ownerGID); err != nil {
		return err
	}

	// Get docker group GID
	dockerGID := getDockerGID()

	// Build mounts — agent binary comes from the Docker image, not a bind mount
	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: volName,
			Target: "/data",
		},
		{
			Type:     mount.TypeBind,
			Source:   "/var/run/docker.sock",
			Target:   "/var/run/docker.sock",
			ReadOnly: false,
		},
	}

	if shouldMountHostHome() {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory for optional host home mount: %w", err)
		}
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   homeDir,
			Target:   homeDir,
			ReadOnly: true,
		})
	}

	// Append caller-supplied extra mounts (e.g. broker socket directory)
	mounts = append(mounts, cfg.ExtraMounts...)

	// Container config
	config := &container.Config{
		Image:        AgentImageName,
		Cmd:          cfg.Cmd,
		Env:          cfg.Env,
		User:         fmt.Sprintf("%d:%d", ownerUID, ownerGID),
		Tty:          true,
		OpenStdin:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Labels:       cfg.Labels,
		WorkingDir:   "/data",
	}

	// Host config
	hostConfig := &container.HostConfig{
		Mounts:      mounts,
		GroupAdd:    []string{dockerGID},
		ExtraHosts:  []string{"host.docker.internal:host-gateway"},
		SecurityOpt: []string{"no-new-privileges"},
		Resources: container.Resources{
			Memory:   cfg.MemoryBytes,
			NanoCPUs: cfg.NanoCPUs,
		},
		NetworkMode: "bridge",
	}

	// Create container
	resp, err := dm.client.ContainerCreate(ctx, config, hostConfig, nil, nil, name)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	if len(resp.Warnings) > 0 {
		for _, warning := range resp.Warnings {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
		}
	}

	return nil
}

// StartContainer starts a container.
func (dm *DockerManager) StartContainer(ctx context.Context, agentID string) error {
	name := ContainerName(agentID)

	if err := dm.client.ContainerStart(ctx, name, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// StopContainer stops a running container.
func (dm *DockerManager) StopContainer(ctx context.Context, agentID string) error {
	name := ContainerName(agentID)

	timeout := 10 // seconds
	if err := dm.client.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	return nil
}

// RemoveContainer stops and removes a container.
func (dm *DockerManager) RemoveContainer(ctx context.Context, agentID string) error {
	name := ContainerName(agentID)

	// Stop container if running
	status := dm.ContainerStatus(ctx, agentID)
	if status == "running" {
		if err := dm.StopContainer(ctx, agentID); err != nil {
			return err
		}
	}

	// Remove container
	if err := dm.client.ContainerRemove(ctx, name, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: false,
	}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// PurgeContainer removes both container and volume.
func (dm *DockerManager) PurgeContainer(ctx context.Context, agentID string) error {
	if os.Getenv(allowVolumePurgeEnv) != "1" {
		return fmt.Errorf("volume purge disabled; set %s=1 to allow", allowVolumePurgeEnv)
	}
	// Remove container first
	if err := dm.RemoveContainer(ctx, agentID); err != nil {
		// Ignore error if container doesn't exist
		if !client.IsErrNotFound(err) {
			return err
		}
	}

	// Remove volume
	volName := VolumeName(agentID)
	if err := dm.client.VolumeRemove(ctx, volName, true); err != nil {
		// Ignore error if volume doesn't exist
		if !client.IsErrNotFound(err) {
			return fmt.Errorf("failed to remove volume: %w", err)
		}
	}

	return nil
}

func shouldMountHostHome() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("GOWILD_ALLOW_HOME_MOUNT")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
