package dockermgr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/docker/docker/client"
)

// EnsureImage rebuilds the gowild-agent Docker image.
// Uses a CACHE_BUST build arg to invalidate source copy and Go build layers
// while keeping expensive layers (apt-get) cached.
func (dm *DockerManager) EnsureImage(ctx context.Context) error {
	// Refresh build info to catch local changes without requiring a manager restart.
	dm.RefreshDesiredBuildInfo()

	if !dm.ImageExists(ctx) {
		return dm.buildAgentImage(ctx)
	}

	if !dm.desiredBuildInfo.Known() {
		return nil
	}

	stale, _, _, err := dm.ImageStale(ctx, AgentImageName)
	if err == nil && !stale {
		return nil
	}

	return dm.buildAgentImage(ctx)
}

// BuildImage forces a rebuild of the gowild-agent image.
func (dm *DockerManager) BuildImage(ctx context.Context) error {
	dm.RefreshDesiredBuildInfo()
	return dm.buildAgentImage(ctx)
}

func (dm *DockerManager) buildAgentImage(ctx context.Context) error {
	// Dockerfile COPY paths are relative to the workspace root (../..).
	dockerfilePath := filepath.Join("..", "agent", "Dockerfile")
	buildContext := filepath.Join("..", "..")
	cacheBust := strconv.FormatInt(time.Now().UnixNano(), 10)
	buildTime := time.Now().UTC().Format(time.RFC3339)
	info := dm.desiredBuildInfo
	if !info.Known() {
		info = BuildInfo{ID: "unknown", SHA: "unknown", Dirty: false}
	}

	cmd := exec.CommandContext(ctx, "docker", "build",
		"-f", dockerfilePath,
		"-t", AgentImageName,
		"--build-arg", "CACHE_BUST="+cacheBust,
		"--build-arg", "BUILD_SHA="+info.SHA,
		"--build-arg", "BUILD_DIRTY="+strconv.FormatBool(info.Dirty),
		"--build-arg", "BUILD_ID="+info.ID,
		"--build-arg", "BUILD_TIME="+buildTime,
		buildContext,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build docker image: %w", err)
	}

	return nil
}

// ImageExists checks if the gowild-agent image exists.
func (dm *DockerManager) ImageExists(ctx context.Context) bool {
	_, _, err := dm.client.ImageInspectWithRaw(ctx, AgentImageName)
	return err == nil
}

// ImageBuildID returns the build ID label for an image (empty if missing).
func (dm *DockerManager) ImageBuildID(ctx context.Context, imageRef string) (string, error) {
	inspect, _, err := dm.client.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return "", err
	}
	if inspect.Config == nil || inspect.Config.Labels == nil {
		return "", nil
	}
	return inspect.Config.Labels[buildLabelID], nil
}

// ImageStale compares an image's build ID to the desired build info.
func (dm *DockerManager) ImageStale(ctx context.Context, imageRef string) (bool, string, string, error) {
	desired := dm.desiredBuildInfo
	if !desired.Known() {
		return false, "", "", nil
	}

	currentID, err := dm.ImageBuildID(ctx, imageRef)
	if err != nil {
		if client.IsErrNotFound(err) {
			return true, "", desired.ID, nil
		}
		return false, "", desired.ID, err
	}
	if currentID == "" {
		return true, "", desired.ID, nil
	}
	return currentID != desired.ID, currentID, desired.ID, nil
}

// AgentImageStale checks the agent's running container image (if present) or the current tag.
func (dm *DockerManager) AgentImageStale(ctx context.Context, agentID string) (bool, string, string, error) {
	status := dm.ContainerStatus(ctx, agentID)
	if status != "" {
		inspect, err := dm.client.ContainerInspect(ctx, ContainerName(agentID))
		if err != nil {
			return false, "", "", err
		}
		return dm.ImageStale(ctx, inspect.Image)
	}
	return dm.ImageStale(ctx, AgentImageName)
}
