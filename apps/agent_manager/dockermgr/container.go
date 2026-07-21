package dockermgr

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// ContainerStatus returns the status of a container.
func (dm *DockerManager) ContainerStatus(ctx context.Context, agentID string) string {
	if dm == nil || dm.client == nil {
		return ""
	}
	name := ContainerName(agentID)

	inspect, err := dm.client.ContainerInspect(ctx, name)
	if err != nil {
		return ""
	}

	return inspect.State.Status
}

// ListContainers lists all gowild-agent containers.
func (dm *DockerManager) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", containerPrefix)

	containers, err := dm.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		agentID := strings.TrimPrefix(name, containerPrefix)

		result = append(result, ContainerInfo{
			AgentID: agentID,
			Name:    name,
			Status:  c.Status,
			State:   c.State,
			Labels:  c.Labels,
			Image:   c.Image,
		})
	}

	return result, nil
}

// InspectContainer returns detailed info about a container including env and cmd.
func (dm *DockerManager) InspectContainer(ctx context.Context, agentID string) (*ContainerInfo, error) {
	cName := ContainerName(agentID)
	inspect, err := dm.client.ContainerInspect(ctx, cName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	return &ContainerInfo{
		AgentID: agentID,
		Name:    cName,
		Status:  inspect.State.Status,
		State:   inspect.State.Status,
		Labels:  inspect.Config.Labels,
		Env:     inspect.Config.Env,
		Image:   inspect.Config.Image,
		Cmd:     append(inspect.Config.Entrypoint, inspect.Config.Cmd...),
	}, nil
}

// AttachContainer attaches to a running container's stdin/stdout.
func (dm *DockerManager) AttachContainer(ctx context.Context, agentID string) (*AttachSession, error) {
	name := ContainerName(agentID)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)

	// Attach to container
	resp, err := dm.client.ContainerAttach(ctx, name, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to attach to container: %w", err)
	}

	return &AttachSession{
		AgentID: agentID,
		Conn:    resp,
		cancel:  cancel,
	}, nil
}

// ContainerLogs retrieves logs from a container.
func (dm *DockerManager) ContainerLogs(ctx context.Context, agentID string, tail int) (string, error) {
	name := ContainerName(agentID)

	tailStr := "all"
	if tail > 0 {
		tailStr = strconv.Itoa(tail)
	}

	logs, err := dm.client.ContainerLogs(ctx, name, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tailStr,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logs.Close()

	// Read all logs
	data, err := io.ReadAll(logs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(data), nil
}

// Close closes the attach session.
func (as *AttachSession) Close() {
	if as.cancel != nil {
		as.cancel()
	}
	as.Conn.Close()
}

// CopyFileToContainer copies a file into the agent's container at /data/uploads/<filename>.
func (dm *DockerManager) CopyFileToContainer(ctx context.Context, agentID, filename string, fileData []byte) error {
	cName := ContainerName(agentID)

	// Build a tar archive containing uploads/<filename>
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "uploads/" + filename,
		Mode: 0644,
		Size: int64(len(fileData)),
	}); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}
	if _, err := tw.Write(fileData); err != nil {
		return fmt.Errorf("failed to write tar data: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Copy into container at /data (so file ends up at /data/uploads/<filename>)
	if err := dm.client.CopyToContainer(ctx, cName, "/data", &buf, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("failed to copy file to container: %w", err)
	}

	return nil
}

// CopyFromContainer reads a file or directory from a container as a tar stream.
func (dm *DockerManager) CopyFromContainer(ctx context.Context, containerName, srcPath string) (io.ReadCloser, container.PathStat, error) {
	return dm.client.CopyFromContainer(ctx, containerName, srcPath)
}
