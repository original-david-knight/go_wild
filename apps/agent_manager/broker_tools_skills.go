package main

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type skillToolHandlerFunc func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

var skillToolHandlers = map[string]skillToolHandlerFunc{
	"save_skill": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		sk := tools.NewSkillsTools(nil, svc) // nil pythonTools — CRUD doesn't need it
		return callWithInput[tools.SaveSkillInput](inputJSON, func(input tools.SaveSkillInput) (any, error) {
			r, err := sk.SaveSkillTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"list_skills": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		sk := tools.NewSkillsTools(nil, svc)
		return callWithInput[tools.ListSkillsInput](inputJSON, func(input tools.ListSkillsInput) (any, error) {
			r, err := sk.ListSkillsTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"get_skill": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		sk := tools.NewSkillsTools(nil, svc)
		return callWithInput[tools.GetSkillInput](inputJSON, func(input tools.GetSkillInput) (any, error) {
			r, err := sk.GetSkillTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"delete_skill": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		sk := tools.NewSkillsTools(nil, svc)
		return callWithInput[tools.DeleteSkillInput](inputJSON, func(input tools.DeleteSkillInput) (any, error) {
			r, err := sk.DeleteSkillTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func isSkillTool(toolName string) bool {
	_, ok := skillToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callSkillTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isSkillTool(toolName) {
		return false, nil, nil
	}

	handler, ok := skillToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(ctx, svc, inputJSON)
	return true, result, err
}
