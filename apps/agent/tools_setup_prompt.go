package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
)

//go:embed default_system_prompt.md
var defaultSystemPrompt string

// loadSystemPrompt loads the system prompt from system_prompt.md if it exists.
// Checks in order: ./system_prompt.md, ~/.gowild_system_prompt.md
// Falls back to defaultSystemPrompt if no file is found.
// Also appends soul.md content if it exists.
// The system prompt is treated as a template with {{AgentName}} replaced by the agent's display name.
func loadSystemPrompt(ctx context.Context, agentID string, runtime *agentRuntime) string {
	var basePrompt string

	// Check current directory first
	if content, err := os.ReadFile("system_prompt.md"); err == nil {
		fmt.Println(color.HiBlackString("System prompt: ./system_prompt.md"))
		basePrompt = string(content)
	} else if home, err := os.UserHomeDir(); err == nil {
		// Check home directory
		homePath := home + "/.gowild_system_prompt.md"
		if content, err := os.ReadFile(homePath); err == nil {
			fmt.Println(color.HiBlackString("System prompt: %s", homePath))
			basePrompt = string(content)
		}
	}

	// Fall back to default if no file found
	if basePrompt == "" {
		basePrompt = defaultSystemPrompt
	}

	// Inject current date/time
	basePrompt = strings.ReplaceAll(basePrompt, "{{current_time}}", time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST"))

	// Get agent display name and apply template substitution
	agentName := getAgentDisplayName(ctx, agentID, runtime)
	basePrompt = applyAgentTemplate(basePrompt, agentName)

	// Inject per-agent configured section
	configuredSection := ""
	if runtime != nil && runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_system_prompt", map[string]any{})
		if err == nil {
			if sp, ok := result["system_prompt"].(string); ok && sp != "" {
				configuredSection = strings.ReplaceAll(sp, "{{AgentName}}", agentName)
			}
		}
	} else if runtime != nil && runtime.service != nil {
		if agent, err := runtime.service.GetAgent(ctx); err == nil && agent != nil && strings.TrimSpace(agent.SystemPrompt) != "" {
			configuredSection = strings.ReplaceAll(agent.SystemPrompt, "{{AgentName}}", agentName)
		}
	}

	// Fetch capabilities and append to configured section.
	// Only include method names and brief descriptions — full schemas are provided
	// when a method job is actually assigned via heartbeat to avoid bloating the system prompt.
	var capabilities []map[string]string
	if runtime != nil && runtime.brokerClient != nil {
		capResult, capErr := runtime.brokerClient.CallTool(ctx, "get_capabilities", map[string]any{})
		if capErr == nil {
			if caps, ok := capResult["capabilities"].([]any); ok {
				for _, c := range caps {
					if cm, ok := c.(map[string]any); ok {
						capabilities = append(capabilities, map[string]string{
							"role":        stringFromMap(cm, "role"),
							"method":      stringFromMap(cm, "method"),
							"description": stringFromMap(cm, "description"),
						})
					}
				}
			}
		}
	} else if runtime != nil && runtime.service != nil {
		if caps, err := runtime.service.GetCapabilities(ctx); err == nil {
			for _, cap := range caps {
				capabilities = append(capabilities, map[string]string{
					"role":        cap.Role,
					"method":      cap.Method,
					"description": cap.Description,
				})
			}
		}
	}
	if len(capabilities) > 0 {
		var capSection strings.Builder
		capSection.WriteString("\n\n## Method Capabilities\n\n")
		capSection.WriteString("You can implement the following methods when they are assigned to you:\n\n")
		for _, cap := range capabilities {
			fmt.Fprintf(&capSection, "- **%s** (role: %s): %s\n", cap["method"], cap["role"], cap["description"])
		}
		capSection.WriteString("\nWhen a method job is assigned to you, the full instructions and schemas will be included in the assignment message.\n")
		configuredSection += capSection.String()
	}

	basePrompt = strings.ReplaceAll(basePrompt, "{{AGENT_CONFIGURED_SECTION}}", configuredSection)

	// Inject company context if agent belongs to a company
	companyName := ""
	companyDesc := ""
	role := ""
	if runtime != nil && runtime.brokerClient != nil {
		companyResult, companyErr := runtime.brokerClient.CallTool(ctx, "get_company_context", map[string]any{})
		if companyErr == nil {
			if hasCompany, _ := companyResult["has_company"].(bool); hasCompany {
				companyName, _ = companyResult["company_name"].(string)
				companyDesc, _ = companyResult["company_description"].(string)
				role, _ = companyResult["role"].(string)
			}
		}
	} else if runtime != nil && runtime.db != nil {
		if company, err := data.GetCompanyForAgent(ctx, runtime.db, agentID); err == nil && company != nil {
			companyName = company.Name
			companyDesc = company.Description
			if member, err := data.GetCompanyMemberForAgent(ctx, runtime.db, agentID); err == nil && member != nil {
				role = member.Role
			}
		}
	}
	if companyName != "" || companyDesc != "" {
		var companySection strings.Builder
		companySection.WriteString("\n\n## Company Context\n\n")
		if companyName != "" {
			fmt.Fprintf(&companySection, "You are a member of **%s**", companyName)
			if role != "" {
				fmt.Fprintf(&companySection, " (role: %s)", role)
			}
			companySection.WriteString(".\n\n")
		}
		if companyDesc != "" {
			fmt.Fprintf(&companySection, "Company description: %s\n", companyDesc)
		}
		basePrompt += companySection.String()
		fmt.Println(color.HiBlackString("Company: %s", companyName))
	}

	// Inject wallet public keys.
	if runtime != nil && runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_wallet_addresses", map[string]any{})
		if err == nil {
			if ethAddr, ok := result["eth_address"].(string); ok && ethAddr != "" {
				basePrompt = strings.ReplaceAll(basePrompt, "{{EthereumAddress}}", ethAddr)
			}
			if solAddr, ok := result["sol_address"].(string); ok && solAddr != "" {
				basePrompt = strings.ReplaceAll(basePrompt, "{{SolanaPublicKey}}", solAddr)
			}
		}
	} else if runtime != nil && runtime.service != nil {
		if seedPhrase, err := runtime.service.GetWalletSeedPhrase(ctx); err == nil && strings.TrimSpace(seedPhrase) != "" {
			if derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0); err == nil {
				basePrompt = strings.ReplaceAll(basePrompt, "{{EthereumAddress}}", derived.EthAddress)
				basePrompt = strings.ReplaceAll(basePrompt, "{{SolanaPublicKey}}", derived.SolAddress)
			}
		}
	}
	// Clear any remaining placeholders if keys weren't available
	basePrompt = strings.ReplaceAll(basePrompt, "{{SolanaPublicKey}}", "(not configured)")
	basePrompt = strings.ReplaceAll(basePrompt, "{{EthereumAddress}}", "(not configured)")

	// Append soul.md content.
	var soulContent string
	if runtime != nil && runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_soul_content", map[string]any{})
		if err == nil {
			if c, ok := result["content"].(string); ok && c != "" {
				soulContent = c
				fmt.Println(color.HiBlackString("Soul: via broker"))
			}
		}
	} else if runtime != nil && runtime.service != nil {
		if soul, err := runtime.service.GetSoul(ctx); err == nil && soul != nil && soul.Content != "" {
			soulContent = soul.Content
			fmt.Println(color.HiBlackString("Soul: direct database"))
		}
	}
	if soulContent != "" {
		basePrompt = basePrompt + "\n\n---\n\n## soul.md\n\n" + soulContent
	}

	// Append pending tasks (always visible block)
	tasksBlock := renderTasksBlock(ctx, runtime)
	if tasksBlock != "" {
		basePrompt = basePrompt + tasksBlock
	}

	return basePrompt
}

// renderTasksBlock renders the always-visible tasks block.
func renderTasksBlock(ctx context.Context, runtime *agentRuntime) string {
	var parts []string

	var taskList []any
	switch {
	case runtime != nil && runtime.brokerClient != nil:
		result, err := runtime.brokerClient.CallTool(ctx, "get_pending_tasks", map[string]any{})
		if err == nil {
			taskList, _ = result["tasks"].([]any)
			if len(taskList) > 0 {
				fmt.Println(color.HiBlackString("Tasks: %d pending (via broker)", len(taskList)))
			}
		}
	case runtime != nil && runtime.service != nil:
		tasks, err := runtime.service.GetPendingTasks(ctx)
		if err == nil {
			taskList = make([]any, 0, len(tasks))
			for _, task := range tasks {
				taskList = append(taskList, map[string]any{
					"id":             task.ID,
					"description":    task.Description,
					"parent_task_id": task.ParentTaskID,
					"blocked":        task.Blocked,
				})
			}
			if len(taskList) > 0 {
				fmt.Println(color.HiBlackString("Tasks: %d pending (direct database)", len(taskList)))
			}
		}
	}
	if len(taskList) > 0 {
		// Separate top-level and subtasks
		type taskEntry struct {
			id, desc, parentID string
			blocked            bool
		}
		var topLevel []taskEntry
		subtasksByParent := make(map[string][]taskEntry)

		for _, item := range taskList {
			if t, ok := item.(map[string]any); ok {
				entry := taskEntry{
					id:       stringFromMap(t, "id"),
					desc:     stringFromMap(t, "description"),
					parentID: stringFromMap(t, "parent_task_id"),
				}
				entry.blocked, _ = t["blocked"].(bool)

				if entry.parentID != "" {
					subtasksByParent[entry.parentID] = append(subtasksByParent[entry.parentID], entry)
				} else {
					topLevel = append(topLevel, entry)
				}
			}
		}

		var sb strings.Builder
		sb.WriteString("## Current Tasks\n\nYou have the following pending tasks. Work on these and mark them done when completed:\n\n")
		count := len(topLevel)
		if count > 10 {
			count = 10
		}
		for i := 0; i < count; i++ {
			t := topLevel[i]
			if t.blocked {
				fmt.Fprintf(&sb, "- **Task %s** [BLOCKED]: %s\n", t.id, t.desc)
			} else {
				fmt.Fprintf(&sb, "- **Task %s**: %s\n", t.id, t.desc)
			}
			// Show subtasks indented
			for _, st := range subtasksByParent[t.id] {
				if st.blocked {
					fmt.Fprintf(&sb, "  - **Subtask %s** [BLOCKED]: %s\n", st.id, st.desc)
				} else {
					fmt.Fprintf(&sb, "  - **Subtask %s**: %s\n", st.id, st.desc)
				}
			}
		}
		if len(topLevel) > count {
			fmt.Fprintf(&sb, "\n...and %d more tasks. Use list_tasks to see all.\n", len(topLevel)-count)
		}
		parts = append(parts, sb.String())
	}

	if len(parts) == 0 {
		return ""
	}

	return "\n\n---\n\n# Current Tasks (Always Visible)\n\n" + strings.Join(parts, "\n\n")
}

// getAgentDisplayName returns the display name for an agent.
// Uses the agent's Name field if set via broker, otherwise capitalizes the agent ID.
func getAgentDisplayName(ctx context.Context, agentID string, runtime *agentRuntime) string {
	if runtime != nil && runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_agent_name", map[string]any{})
		if err == nil {
			if name, ok := result["name"].(string); ok && name != "" && name != agentID {
				return name
			}
		}
	}
	if runtime != nil && runtime.service != nil {
		agent, err := runtime.service.GetAgent(ctx)
		if err == nil && agent != nil && strings.TrimSpace(agent.Name) != "" && agent.Name != agentID {
			return agent.Name
		}
	}
	// Default: capitalize the first letter of the agent ID
	if len(agentID) == 0 {
		return "Agent"
	}
	return strings.ToUpper(agentID[:1]) + agentID[1:]
}

// stringFromMap safely extracts a string value from a map.
func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// applyAgentTemplate replaces template placeholders in the system prompt.
// Supports {{AgentName}} and also replaces "Jake" with the agent name for backward compatibility.
func applyAgentTemplate(prompt string, agentName string) string {
	// Replace explicit template placeholder
	prompt = strings.ReplaceAll(prompt, "{{AgentName}}", agentName)

	// Replace hardcoded "Jake" references for backward compatibility with existing prompts
	// This allows the default system_prompt.md to work as a template
	prompt = strings.ReplaceAll(prompt, "You are Jake.", "You are "+agentName+".")
	prompt = strings.ReplaceAll(prompt, "# You are Jake.", "# You are "+agentName+".")
	prompt = strings.ReplaceAll(prompt, "Jake_World_Modeler", agentName+"_World_Modeler")

	return prompt
}
