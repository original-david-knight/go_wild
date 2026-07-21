package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fatih/color"
)

// EnsureSandboxImage builds the sandbox image if it doesn't exist or if rebuild is requested.
func EnsureSandboxImage(ctx context.Context, rebuild bool) error {
	// Check if image exists
	if !rebuild {
		cmd := exec.CommandContext(ctx, "docker", "image", "inspect", imageName())
		if err := cmd.Run(); err == nil {
			return nil // Image exists
		}
	}

	fmt.Println(color.CyanString("Building sandbox image..."))

	// Find the workspace root (two levels above apps/agent)
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// If we're in apps/agent, go up two levels to the workspace root
	buildDir := wd
	if filepath.Base(wd) == "agent" && filepath.Base(filepath.Dir(wd)) == "apps" {
		buildDir = filepath.Dir(filepath.Dir(wd))
	}

	// Build the image (Dockerfile COPY paths are relative to workspace root)
	args := []string{
		"build",
		"-t", imageName(),
		"-f", filepath.Join(buildDir, "apps", "agent", "Dockerfile"),
		buildDir,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}

	fmt.Println(color.GreenString("Sandbox image built successfully"))
	return nil
}

// EnsureVolume creates the agent's data volume if it doesn't exist.
func EnsureVolume(ctx context.Context, agentID string) error {
	volName := volumeName(agentID)

	// Check if volume exists
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", volName)
	if err := cmd.Run(); err == nil {
		return nil // Volume exists
	}

	// Create volume
	cmd = exec.CommandContext(ctx, "docker", "volume", "create", volName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create volume: %w - %s", err, stderr.String())
	}

	fmt.Println(color.HiBlackString("Created volume: %s", volName))
	return nil
}
