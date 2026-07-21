package routes

import (
	"errors"
	"strings"
)

var ErrPeerGroupIDRequired = errors.New("group ID required")

type PeerGroupRoute struct {
	GroupID string
	Action  string
	AgentID string
}

func ParsePeerGroup(path string) (PeerGroupRoute, error) {
	trimmed := strings.TrimPrefix(path, "/api/peer-groups/")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return PeerGroupRoute{}, ErrPeerGroupIDRequired
	}

	route := PeerGroupRoute{
		GroupID: strings.TrimSpace(parts[0]),
	}
	if len(parts) > 1 {
		route.Action = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		route.AgentID = parts[2]
	}
	return route, nil
}
