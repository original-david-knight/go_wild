package dockermgr

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// EnsureVolume creates a volume for the agent if it doesn't exist.
func (dm *DockerManager) EnsureVolume(ctx context.Context, agentID string) error {
	volName := VolumeName(agentID)

	// Check if volume exists
	_, err := dm.client.VolumeInspect(ctx, volName)
	if err == nil {
		return nil // Volume already exists
	}

	// Create volume
	_, err = dm.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: volName,
		Labels: map[string]string{
			"gowild.agent-id": agentID,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create volume: %w", err)
	}

	return nil
}

// VolumeExists checks if the agent volume exists.
func (dm *DockerManager) VolumeExists(ctx context.Context, agentID string) (bool, error) {
	volName := VolumeName(agentID)
	_, err := dm.client.VolumeInspect(ctx, volName)
	if err == nil {
		return true, nil
	}
	if client.IsErrNotFound(err) {
		return false, nil
	}
	return false, err
}

func (dm *DockerManager) ensureVolumeOwnership(ctx context.Context, agentID string, uid, gid int) error {
	if uid < 0 || gid < 0 {
		return fmt.Errorf("invalid uid/gid for volume ownership migration: %d:%d", uid, gid)
	}
	// When manager runs as root, default Docker ownership already matches.
	if uid == 0 && gid == 0 {
		return nil
	}

	markerName := fmt.Sprintf(".gowild-owner-uid%d-gid%d", uid, gid)
	cmd := fmt.Sprintf(
		"set -e; if [ -f /data/%s ]; then exit 0; fi; chown -R %d:%d /data; : > /data/%s; chown %d:%d /data/%s",
		markerName, uid, gid, markerName, uid, gid, markerName,
	)

	resp, err := dm.client.ContainerCreate(
		ctx,
		&container.Config{
			Image:      AgentImageName,
			Entrypoint: []string{"/bin/sh", "-lc"},
			Cmd:        []string{cmd},
			User:       "0:0",
		},
		&container.HostConfig{
			Mounts: []mount.Mount{{
				Type:   mount.TypeVolume,
				Source: VolumeName(agentID),
				Target: "/data",
			}},
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create volume ownership migration container: %w", err)
	}
	defer func() {
		_ = dm.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: false,
		})
	}()

	if err := dm.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start volume ownership migration container: %w", err)
	}

	waitCh, errCh := dm.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case status := <-waitCh:
		if status.Error != nil && strings.TrimSpace(status.Error.Message) != "" {
			return fmt.Errorf("volume ownership migration failed: %s", strings.TrimSpace(status.Error.Message))
		}
		if status.StatusCode != 0 {
			logTail := migrationContainerLogs(ctx, dm.client, resp.ID)
			if logTail != "" {
				return fmt.Errorf("volume ownership migration exited with code %d: %s", status.StatusCode, logTail)
			}
			return fmt.Errorf("volume ownership migration exited with code %d", status.StatusCode)
		}
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("failed waiting for volume ownership migration container: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func migrationContainerLogs(ctx context.Context, cli Client, containerID string) string {
	logs, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "100",
	})
	if err != nil {
		return ""
	}
	defer logs.Close()

	raw, err := io.ReadAll(logs)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
