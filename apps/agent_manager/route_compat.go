package main

import (
	mgrroutes "github.com/original-david-knight/go_wild/apps/agent_manager/internal/routes"
)

type agentRoute struct {
	agentID      string
	action       string
	taskID       string
	capID        string
	serverID     string
	serverAction string
}

type companyRoute struct {
	companyID string
	action    string
	parts     []string
}

type peerGroupRoute struct {
	groupID string
	action  string
	agentID string
}

type pipelineRouteKind string

const (
	pipelineRouteCollection     pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteCollection)
	pipelineRouteCapabilities   pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteCapabilities)
	pipelineRouteInitialRequest pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteInitialRequest)
	pipelineRouteDefinition     pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteDefinition)
	pipelineRouteTrigger        pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteTrigger)
	pipelineRouteTriggerPolymkt pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteTriggerPolymkt)
	pipelineRouteAction         pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteAction)
	pipelineRouteUnknown        pipelineRouteKind = pipelineRouteKind(mgrroutes.PipelineRouteUnknown)
)

type pipelineRoute struct {
	kind       pipelineRouteKind
	pipelineID string
	action     string
}

var errPeerGroupIDRequired = mgrroutes.ErrPeerGroupIDRequired
var errPipelineIDRequired = mgrroutes.ErrPipelineIDRequired

func parseAgentRoute(path string) (agentRoute, bool) {
	parsed, err := mgrroutes.ParseAgent(path)
	if err != nil {
		return agentRoute{}, false
	}
	return agentRoute{
		agentID:      parsed.AgentID,
		action:       parsed.Action,
		taskID:       parsed.TaskID,
		capID:        parsed.CapID,
		serverID:     parsed.ServerID,
		serverAction: parsed.ServerAction,
	}, true
}

func parseCompanyRoute(path string) (companyRoute, error) {
	parsed, err := mgrroutes.ParseCompany(path)
	if err != nil {
		return companyRoute{}, err
	}
	return companyRoute{
		companyID: parsed.CompanyID,
		action:    parsed.Action,
		parts:     append([]string(nil), parsed.Parts...),
	}, nil
}

func parsePeerGroupRoute(path string) (peerGroupRoute, error) {
	parsed, err := mgrroutes.ParsePeerGroup(path)
	if err != nil {
		return peerGroupRoute{}, err
	}
	return peerGroupRoute{
		groupID: parsed.GroupID,
		action:  parsed.Action,
		agentID: parsed.AgentID,
	}, nil
}

func parsePipelineRoute(path string) (pipelineRoute, error) {
	parsed, err := mgrroutes.ParsePipeline(path)
	if err != nil {
		return pipelineRoute{}, err
	}
	return pipelineRoute{
		kind:       pipelineRouteKind(parsed.Kind),
		pipelineID: parsed.PipelineID,
		action:     parsed.Action,
	}, nil
}
