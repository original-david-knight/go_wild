package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelectHyprlandInstancePrefersWaylandSocket(t *testing.T) {
	data := []byte(`[
		{"instance":"newer","time":20,"pid":2,"wl_socket":"wayland-2"},
		{"instance":"matching","time":10,"pid":1,"wl_socket":"wayland-1"}
	]`)
	got, err := selectHyprlandInstance(data, "", "wayland-1")
	if err != nil {
		t.Fatalf("selectHyprlandInstance returned error: %v", err)
	}
	if got.Instance != "matching" {
		t.Fatalf("selected instance = %q, want matching", got.Instance)
	}
}

func TestSelectHyprlandInstancePrefersSignature(t *testing.T) {
	data := []byte(`[
		{"instance":"newer","time":20,"pid":2,"wl_socket":"wayland-2"},
		{"instance":"matching","time":10,"pid":1,"wl_socket":"wayland-1"}
	]`)
	got, err := selectHyprlandInstance(data, "matching", "")
	if err != nil {
		t.Fatalf("selectHyprlandInstance returned error: %v", err)
	}
	if got.Instance != "matching" || got.WLSocket != "wayland-1" {
		t.Fatalf("selected instance = %#v, want matching signature", got)
	}
}

func TestSelectHyprlandInstanceUsesSoleRunningInstance(t *testing.T) {
	data := []byte(`[{"instance":"only","time":10,"pid":1,"wl_socket":"wayland-1"}]`)
	got, err := selectHyprlandInstance(data, "", "")
	if err != nil {
		t.Fatalf("selectHyprlandInstance returned error: %v", err)
	}
	if got.Instance != "only" || got.WLSocket != "wayland-1" {
		t.Fatalf("selected instance = %#v, want sole instance", got)
	}
}

func TestSelectHyprlandInstanceRejectsMissingOrAmbiguousInstances(t *testing.T) {
	if _, err := selectHyprlandInstance([]byte(`[]`), "", ""); err == nil {
		t.Fatalf("expected no-running-instance error")
	}
	data := []byte(`[
		{"instance":"one","time":10,"pid":1,"wl_socket":"wayland-1"},
		{"instance":"two","time":20,"pid":2,"wl_socket":"wayland-2"}
	]`)
	if _, err := selectHyprlandInstance(data, "", ""); err == nil {
		t.Fatalf("expected multiple-running-instances error")
	}
	if _, err := selectHyprlandInstance(data, "stale", ""); err == nil {
		t.Fatalf("expected stale configured instance error")
	}
}

func TestSetEnvValueReplacesExistingValue(t *testing.T) {
	got := setEnvValue([]string{"A=1", "B=2", "A=old"}, "A", "new")
	want := []string{"B=2", "A=new"}
	if len(got) != len(want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environment = %#v, want %#v", got, want)
		}
	}
}

func TestOSCommandRunnerDiscoversHyprlandEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh shebang scripts; Hyprland env discovery is not used on Windows")
	}
	dir := t.TempDir()
	hyprctl := filepath.Join(dir, "hyprctl")
	grim := filepath.Join(dir, "grim")
	if err := os.WriteFile(hyprctl, []byte(`#!/bin/sh
if [ "$1" = "instances" ] && [ "$2" = "-j" ]; then
  printf '[{"instance":"test-signature","time":1,"pid":1,"wl_socket":"wayland-test"}]'
  exit 0
fi
printf '%s|%s' "$HYPRLAND_INSTANCE_SIGNATURE" "$WAYLAND_DISPLAY"
`), 0o700); err != nil {
		t.Fatalf("write fake hyprctl: %v", err)
	}
	if err := os.WriteFile(grim, []byte(`#!/bin/sh
printf '%s|%s' "$HYPRLAND_INSTANCE_SIGNATURE" "$WAYLAND_DISPLAY"
`), 0o700); err != nil {
		t.Fatalf("write fake grim: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	runner := OSCommandRunner{Getenv: func(string) string { return "" }}

	for _, command := range []string{"hyprctl", "grim"} {
		out, err := runner.Run(context.Background(), command, "test")
		if err != nil {
			t.Fatalf("run %s: %v", command, err)
		}
		if got := strings.TrimSpace(string(out)); got != "test-signature|wayland-test" {
			t.Fatalf("%s environment = %q", command, got)
		}
	}
}
