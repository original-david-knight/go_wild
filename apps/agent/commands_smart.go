package main

import (
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
)

func handleSmartCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	setSmartMode := func(enabled bool) {
		if enabled && strings.TrimSpace(ctx.models.smart) == "" {
			output.SystemWarning("Smart mode unavailable: smart model is not configured.")
			return
		}
		*ctx.smartMode = enabled
		if enabled {
			ctx.agent.SetModel(ctx.models.smart)
			ctx.agent.SetThinkingBudget(smartThinkingBudget)
			output.SmartMode(true, ctx.models.smart)
			output.System("🧠 Smart mode ON")
			output.System("   Model: %s", ctx.models.smart)
			output.System("   Thinking budget: %d tokens", smartThinkingBudget)
		} else {
			ctx.agent.SetModel(ctx.models.base)
			ctx.agent.SetThinkingBudget(normalThinkingBudget)
			output.SmartMode(false, ctx.models.base)
			output.System("⚡ Smart mode OFF (fast mode)")
			output.System("   Model: %s", ctx.models.base)
		}
	}
	emitSmartStatus := func() {
		model := ctx.models.base
		if *ctx.smartMode {
			model = ctx.models.smart
		}
		output.SmartMode(*ctx.smartMode, model)
	}

	// Structured control: request status or explicitly set state.
	if cm.Args != nil {
		if v, ok := cm.Args["status"]; ok {
			if b, ok := v.(bool); ok && b {
				emitSmartStatus()
				return cmdContinue
			}
		}
		if v, ok := cm.Args["action"].(string); ok {
			action := strings.ToLower(strings.TrimSpace(v))
			if action == "status" || action == "state" {
				emitSmartStatus()
				return cmdContinue
			}
		}
		if v, ok := cm.Args["enabled"]; ok {
			switch val := v.(type) {
			case bool:
				setSmartMode(val)
				return cmdContinue
			case string:
				switch strings.ToLower(strings.TrimSpace(val)) {
				case "on", "true", "1", "yes":
					setSmartMode(true)
					return cmdContinue
				case "off", "false", "0", "no":
					setSmartMode(false)
					return cmdContinue
				}
			}
		}
	}

	// Default behavior: toggle.
	setSmartMode(!*ctx.smartMode)
	return cmdContinue
}
