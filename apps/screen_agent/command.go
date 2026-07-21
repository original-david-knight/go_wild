package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(name string) (string, error)
}

type OSCommandRunner struct {
	Getenv func(string) string
}

func (r OSCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	env, err := r.desktopCommandEnv(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return runOSCommand(ctx, env, name, args...)
}

func runOSCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func (OSCommandRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

type hyprlandInstance struct {
	Instance string `json:"instance"`
	Time     int64  `json:"time"`
	PID      int    `json:"pid"`
	WLSocket string `json:"wl_socket"`
}

func (r OSCommandRunner) desktopCommandEnv(ctx context.Context, name string, args []string) ([]string, error) {
	if !needsHyprlandEnvironment(name) || isHyprlandInstancesCommand(name, args) {
		return nil, nil
	}
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	signature := strings.TrimSpace(getenv("HYPRLAND_INSTANCE_SIGNATURE"))
	waylandDisplay := strings.TrimSpace(getenv("WAYLAND_DISPLAY"))
	needSignature := name == "hyprctl" && signature == ""
	needWaylandDisplay := (name == "grim" || name == "slurp") && waylandDisplay == ""
	if needSignature || needWaylandDisplay {
		out, err := runOSCommand(ctx, nil, "hyprctl", "instances", "-j")
		if err != nil {
			return nil, fmt.Errorf("discover Hyprland instance: %w", err)
		}
		instance, err := selectHyprlandInstance(out, signature, waylandDisplay)
		if err != nil {
			return nil, err
		}
		if signature == "" {
			signature = instance.Instance
		}
		if waylandDisplay == "" {
			waylandDisplay = instance.WLSocket
		}
	}

	env := os.Environ()
	if signature != "" {
		env = setEnvValue(env, "HYPRLAND_INSTANCE_SIGNATURE", signature)
	}
	if waylandDisplay != "" {
		env = setEnvValue(env, "WAYLAND_DISPLAY", waylandDisplay)
	}
	return env, nil
}

func needsHyprlandEnvironment(name string) bool {
	switch name {
	case "hyprctl", "grim", "slurp":
		return true
	default:
		return false
	}
}

func isHyprlandInstancesCommand(name string, args []string) bool {
	return name == "hyprctl" && len(args) == 2 && args[0] == "instances" && args[1] == "-j"
}

func selectHyprlandInstance(data []byte, preferredSignature, preferredSocket string) (hyprlandInstance, error) {
	var instances []hyprlandInstance
	if err := json.Unmarshal(data, &instances); err != nil {
		return hyprlandInstance{}, fmt.Errorf("parse hyprctl instances: %w", err)
	}
	preferredSignature = strings.TrimSpace(preferredSignature)
	preferredSocket = strings.TrimSpace(preferredSocket)
	valid := make([]hyprlandInstance, 0, len(instances))
	for _, instance := range instances {
		if strings.TrimSpace(instance.Instance) == "" || strings.TrimSpace(instance.WLSocket) == "" {
			continue
		}
		valid = append(valid, instance)
		if preferredSignature != "" && instance.Instance == preferredSignature {
			return instance, nil
		}
		if preferredSocket != "" && instance.WLSocket == preferredSocket {
			return instance, nil
		}
	}
	if preferredSignature != "" || preferredSocket != "" {
		return hyprlandInstance{}, fmt.Errorf("discover Hyprland instance: configured instance is not running")
	}
	if len(valid) == 0 {
		return hyprlandInstance{}, fmt.Errorf("discover Hyprland instance: no running instance found")
	}
	if len(valid) > 1 {
		return hyprlandInstance{}, fmt.Errorf("discover Hyprland instance: multiple running instances found; set HYPRLAND_INSTANCE_SIGNATURE")
	}
	return valid[0], nil
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

type CommandSpec struct {
	Name string
	Args []string
}

func ParseCommandSpec(raw string) (CommandSpec, error) {
	parts, err := splitCommandLine(raw)
	if err != nil {
		return CommandSpec{}, err
	}
	if len(parts) == 0 {
		return CommandSpec{}, fmt.Errorf("empty command")
	}
	return CommandSpec{Name: parts[0], Args: parts[1:]}, nil
}

func splitCommandLine(raw string) ([]string, error) {
	var out []string
	var buf bytes.Buffer
	var quote rune
	escaped := false
	inToken := false

	for _, r := range raw {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped = false
			inToken = true
		case r == '\\':
			escaped = true
			inToken = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
			inToken = true
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case unicode.IsSpace(r):
			if inToken {
				out = append(out, buf.String())
				buf.Reset()
				inToken = false
			}
		default:
			buf.WriteRune(r)
			inToken = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("unfinished escape in command")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in command")
	}
	if inToken {
		out = append(out, buf.String())
	}
	return out, nil
}

type DebugLogger struct {
	Enabled bool
	Out     io.Writer
}

func (l DebugLogger) Printf(format string, args ...any) {
	if !l.Enabled || l.Out == nil {
		return
	}
	fmt.Fprintf(l.Out, "screen-agent: "+format+"\n", args...)
}
