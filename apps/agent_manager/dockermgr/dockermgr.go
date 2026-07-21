package dockermgr

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	// AgentImageName is the Docker image used for agent containers.
	AgentImageName  = "gowild-agent:latest"
	containerPrefix = "gowild-agent-"
	volumePrefix    = "gowild-agent-"
	volumeSuffix    = "-data"

	allowVolumePurgeEnv = "ALLOW_VOLUME_PURGE"

	buildLabelID    = "org.gowild.agent.build_id"
	buildLabelSHA   = "org.gowild.agent.build_sha"
	buildLabelDirty = "org.gowild.agent.build_dirty"
	buildLabelTime  = "org.gowild.agent.build_time"
)

// Client is the Docker SDK client interface abstraction.
type Client interface {
	ImageInspectWithRaw(ctx context.Context, image string) (types.ImageInspect, []byte, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
	VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeRemove(ctx context.Context, volumeID string, force bool) error
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerAttach(ctx context.Context, container string, options container.AttachOptions) (types.HijackedResponse, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	CopyToContainer(ctx context.Context, container, path string, content io.Reader, options container.CopyToContainerOptions) error
	CopyFromContainer(ctx context.Context, container, srcPath string) (io.ReadCloser, container.PathStat, error)
	Ping(ctx context.Context) (types.Ping, error)
	Close() error
}

// DockerManager manages Docker containers for agents.
type DockerManager struct {
	client           Client
	desiredBuildInfo BuildInfo
}

// AttachSession represents a Docker container attach session.
type AttachSession struct {
	AgentID string
	Conn    types.HijackedResponse
	cancel  context.CancelFunc
}

// ContainerInfo holds metadata about a Docker container.
type ContainerInfo struct {
	AgentID string            `json:"agent_id"`
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	State   string            `json:"state"`
	Labels  map[string]string `json:"-"`
	Env     []string          `json:"-"`
	Image   string            `json:"image"`
	Cmd     []string          `json:"-"`
}

// NewDockerManager creates a new Docker SDK client.
func NewDockerManager() (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &DockerManager{client: cli, desiredBuildInfo: ComputeDesiredBuildInfo()}, nil
}

// Close closes the Docker client.
func (dm *DockerManager) Close() error {
	if dm.client != nil {
		return dm.client.Close()
	}
	return nil
}

// RefreshDesiredBuildInfo recomputes the desired build info from the working tree.
func (dm *DockerManager) RefreshDesiredBuildInfo() BuildInfo {
	info := ComputeDesiredBuildInfo()
	if info.Known() {
		dm.desiredBuildInfo = info
	}
	return dm.desiredBuildInfo
}

// DesiredBuildInfo returns the desired agent image build info.
func (dm *DockerManager) DesiredBuildInfo() BuildInfo {
	return dm.desiredBuildInfo
}

// Ping checks Docker daemon connectivity.
func (dm *DockerManager) Ping(ctx context.Context) (types.Ping, error) {
	return dm.client.Ping(ctx)
}
