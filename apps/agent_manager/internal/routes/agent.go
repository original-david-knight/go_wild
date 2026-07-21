package routes

import (
	"errors"
	"strings"
)

var ErrAgentIDRequired = errors.New("agent ID required")

type AgentRoute struct {
	AgentID      string
	Action       string
	TaskID       string
	CapID        string
	ServerID     string
	ServerAction string
}

func ParseAgent(path string) (AgentRoute, error) {
	trimmed := strings.TrimPrefix(path, "/api/agents/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return AgentRoute{}, ErrAgentIDRequired
	}

	route := AgentRoute{AgentID: parts[0]}
	if len(parts) > 1 {
		route.Action = parts[1]
	}

	if strings.HasPrefix(route.Action, "recurring-tasks/") {
		route.TaskID = strings.TrimPrefix(route.Action, "recurring-tasks/")
		route.Action = "recurring-tasks"
	}
	if strings.HasPrefix(route.Action, "capabilities/") {
		route.CapID = strings.TrimPrefix(route.Action, "capabilities/")
		route.Action = "capabilities"
	}
	if strings.HasPrefix(route.Action, "mcp-servers/") {
		rest := strings.TrimPrefix(route.Action, "mcp-servers/")
		serverParts := strings.SplitN(rest, "/", 2)
		route.ServerID = serverParts[0]
		if len(serverParts) > 1 {
			route.ServerAction = serverParts[1]
		}
		route.Action = "mcp-servers"
	}

	return route, nil
}
