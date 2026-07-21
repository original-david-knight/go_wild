package dockermgr

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

func dockerClientOrSkip(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker not available: %v", err)
	}
	return cli
}

func ensureImage(ctx context.Context, t *testing.T, cli *client.Client, imageRef string) {
	t.Helper()
	if _, _, err := cli.ImageInspectWithRaw(ctx, imageRef); err == nil {
		return
	} else if !client.IsErrNotFound(err) {
		t.Fatalf("image inspect failed: %v", err)
	}

	reader, err := cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		t.Skipf("image pull failed: %v", err)
		return
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
}

func TestRemoveContainerPreservesVolume(t *testing.T) {
	cli := dockerClientOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const img = "busybox:1.36"
	ensureImage(ctx, t, cli, img)

	agentID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	dm := &DockerManager{client: cli}

	if err := dm.EnsureVolume(ctx, agentID); err != nil {
		t.Fatalf("ensure volume: %v", err)
	}
	volName := VolumeName(agentID)
	defer func() {
		_ = cli.VolumeRemove(context.Background(), volName, true)
	}()

	createResp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: img,
			Cmd:   []string{"sh", "-c", "echo ok > /data/test.txt"},
		},
		&container.HostConfig{
			Mounts: []mount.Mount{{
				Type:   mount.TypeVolume,
				Source: volName,
				Target: "/data",
			}},
		},
		nil, nil, ContainerName(agentID),
	)
	if err != nil {
		t.Fatalf("container create: %v", err)
	}
	defer func() {
		_ = cli.ContainerRemove(context.Background(), createResp.ID, container.RemoveOptions{Force: true, RemoveVolumes: false})
	}()

	if err := cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("container start: %v", err)
	}
	statusCh, errCh := cli.ContainerWait(ctx, createResp.ID, container.WaitConditionNotRunning)
	select {
	case <-statusCh:
	case err := <-errCh:
		if err != nil {
			t.Fatalf("container wait error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for container")
	}

	if err := dm.RemoveContainer(ctx, agentID); err != nil {
		t.Fatalf("remove container: %v", err)
	}

	exists, err := dm.VolumeExists(ctx, agentID)
	if err != nil {
		t.Fatalf("volume exists check failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected volume %s to exist after container removal", volName)
	}

	readResp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: img,
			Cmd:   []string{"sh", "-c", "cat /data/test.txt"},
		},
		&container.HostConfig{
			Mounts: []mount.Mount{{
				Type:   mount.TypeVolume,
				Source: volName,
				Target: "/data",
			}},
		},
		nil, nil, ContainerName(agentID)+"-read",
	)
	if err != nil {
		t.Fatalf("container create (read): %v", err)
	}
	defer func() {
		_ = cli.ContainerRemove(context.Background(), readResp.ID, container.RemoveOptions{Force: true, RemoveVolumes: false})
	}()

	if err := cli.ContainerStart(ctx, readResp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("container start (read): %v", err)
	}
	statusCh2, errCh2 := cli.ContainerWait(ctx, readResp.ID, container.WaitConditionNotRunning)
	select {
	case <-statusCh2:
	case err := <-errCh2:
		if err != nil {
			t.Fatalf("container wait error (read): %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for container (read)")
	}

	logs, err := cli.ContainerLogs(ctx, readResp.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		t.Fatalf("container logs: %v", err)
	}
	defer logs.Close()
	data, _ := io.ReadAll(logs)
	if !strings.Contains(string(data), "ok") {
		t.Fatalf("expected log output to contain 'ok', got: %s", string(data))
	}
}

func TestPurgeContainerRequiresEnv(t *testing.T) {
	t.Setenv(allowVolumePurgeEnv, "")
	dm := &DockerManager{}
	if err := dm.PurgeContainer(context.Background(), "any"); err == nil {
		t.Fatalf("expected purge to be blocked without %s", allowVolumePurgeEnv)
	}
}
