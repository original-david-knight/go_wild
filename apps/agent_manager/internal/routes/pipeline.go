package routes

import (
	"errors"
	"strings"
)

var ErrPipelineIDRequired = errors.New("pipeline ID is required")

type PipelineRouteKind string

const (
	PipelineRouteCollection     PipelineRouteKind = "collection"
	PipelineRouteCapabilities   PipelineRouteKind = "capabilities"
	PipelineRouteInitialRequest PipelineRouteKind = "initial-request"
	PipelineRouteDefinition     PipelineRouteKind = "definition"
	PipelineRouteTrigger        PipelineRouteKind = "trigger"
	PipelineRouteTriggerPolymkt PipelineRouteKind = "trigger-polymarket"
	PipelineRouteAction         PipelineRouteKind = "action"
	PipelineRouteUnknown        PipelineRouteKind = "unknown"
)

type PipelineRoute struct {
	Kind       PipelineRouteKind
	PipelineID string
	Action     string
}

func ParsePipeline(path string) (PipelineRoute, error) {
	trimmed := strings.TrimPrefix(path, "/api/pipelines")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return PipelineRoute{Kind: PipelineRouteCollection}, nil
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		switch parts[0] {
		case "capabilities":
			return PipelineRoute{Kind: PipelineRouteCapabilities}, nil
		case "initial-request":
			return PipelineRoute{Kind: PipelineRouteInitialRequest}, nil
		}
		pipelineID := strings.TrimSpace(parts[0])
		if pipelineID == "" {
			return PipelineRoute{}, ErrPipelineIDRequired
		}
		return PipelineRoute{Kind: PipelineRouteDefinition, PipelineID: pipelineID}, nil
	}

	pipelineID := strings.TrimSpace(parts[0])
	if pipelineID == "" {
		return PipelineRoute{}, ErrPipelineIDRequired
	}
	if len(parts) == 2 {
		action := strings.TrimSpace(parts[1])
		switch action {
		case "trigger":
			return PipelineRoute{Kind: PipelineRouteTrigger, PipelineID: pipelineID, Action: action}, nil
		case "trigger-polymarket":
			return PipelineRoute{Kind: PipelineRouteTriggerPolymkt, PipelineID: pipelineID, Action: action}, nil
		}
	}
	if len(parts) == 3 && strings.TrimSpace(parts[1]) == "actions" {
		action := strings.TrimSpace(parts[2])
		if action == "" {
			return PipelineRoute{Kind: PipelineRouteUnknown, PipelineID: pipelineID}, nil
		}
		return PipelineRoute{Kind: PipelineRouteAction, PipelineID: pipelineID, Action: action}, nil
	}

	return PipelineRoute{Kind: PipelineRouteUnknown, PipelineID: pipelineID}, nil
}
