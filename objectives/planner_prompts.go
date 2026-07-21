package objectives

import (
	"fmt"
	"strings"
)

// MaxTreeDepth is the maximum depth allowed for the objective tree.
// Objectives at this depth must produce execution nodes, not more children.
const MaxTreeDepth = 4

// buildTacticalPrompt constructs the prompt for tactical planning.
// It includes the current objective tree context, memory, and tool catalog.
func buildTacticalPrompt(objective *Objective, children []*Objective, toolCatalog string, memory string) string {
	var sb strings.Builder

	sb.WriteString("You are a tactical planner for an autonomous objective management system.\n")
	sb.WriteString("Your job is to decompose a goal into executable tasks and produce tree mutations.\n\n")

	sb.WriteString("## Current Objective\n")
	fmt.Fprintf(&sb, "ID: %s\n", objective.ID)
	fmt.Fprintf(&sb, "Title: %s\n", objective.Title)
	fmt.Fprintf(&sb, "Description: %s\n", objective.Description)
	fmt.Fprintf(&sb, "Status: %s\n", objective.Status)
	fmt.Fprintf(&sb, "Depth: %d\n\n", objective.Depth)

	if len(children) > 0 {
		sb.WriteString("## Existing Children\n")
		for _, c := range children {
			fmt.Fprintf(&sb, "- [%s] %s (status: %s, priority: %d)\n", c.ID, c.Title, c.Status, c.Priority)
			if c.Description != "" {
				fmt.Fprintf(&sb, "  Description: %s\n", c.Description)
			}
		}
		sb.WriteString("\n")
	}

	if toolCatalog != "" {
		sb.WriteString("## Available Tools\n")
		sb.WriteString(toolCatalog)
		sb.WriteString("\n")
	}

	if memory != "" {
		sb.WriteString("## Relevant Memory\n")
		sb.WriteString(memory)
		sb.WriteString("\n")
	}

	atMaxDepth := objective.Depth >= MaxTreeDepth

	sb.WriteString("## Instructions\n")
	if atMaxDepth {
		sb.WriteString("CRITICAL: This objective is at the MAXIMUM tree depth. You MUST NOT create child objectives.\n")
		sb.WriteString("Do NOT produce any 'add' mutations. Instead, produce execution_nodes that accomplish this task directly.\n")
		sb.WriteString("The mutations array should be empty or contain only 'update'/'remove' actions.\n\n")
	}
	sb.WriteString("1. Analyze the objective and decide how to accomplish it.\n")
	if !atMaxDepth {
		sb.WriteString("2. If the task is too broad, produce tree mutations to create 2-4 sub-goals.\n")
		sb.WriteString("   - Use action 'add' with parent_id set to the current objective's ID to create children.\n")
		sb.WriteString("   - Use action 'update' to modify existing children if needed.\n")
		sb.WriteString("   - Use action 'remove' to remove children that are no longer relevant.\n")
		sb.WriteString("   - WARNING: Do NOT decompose further if the task is already concrete and actionable.\n")
	} else {
		sb.WriteString("2. Do NOT create sub-goals. This task must be executed directly.\n")
	}
	sb.WriteString("3. Produce execution_nodes to accomplish the work.\n")
	sb.WriteString("   - Build a small DAG of 1-5 nodes that accomplish the objective.\n")
	sb.WriteString("   - Nodes can depend on other nodes via 'depends_on' to form a DAG.\n")
	sb.WriteString("   - Use 'agentic' type for tasks that need web search, web reading, or HTTP access.\n")
	sb.WriteString("   - Use 'single_shot' type ONLY for pure reasoning/analysis with no tool access.\n")
	sb.WriteString("   - Every node ID must be unique within the graph.\n")
	sb.WriteString("4. CRITICAL — Tool assignment for execution nodes:\n")
	sb.WriteString("   - Every 'agentic' node MUST have a non-empty 'tools' array.\n")
	sb.WriteString("   - Use EXACT tool names from the Available Tools list above.\n")
	sb.WriteString("   - Assign only the tools each node actually needs (not all tools to every node).\n")
	sb.WriteString("   - Nodes that need to search the web: include 'web_search'.\n")
	sb.WriteString("   - Nodes that need to read web pages: include 'read_webpage'.\n")
	sb.WriteString("   - Nodes that need to call APIs: include 'http_request'.\n")
	sb.WriteString("   - A node with no tools runs as single_shot regardless of its type.\n")
	sb.WriteString("5. CRITICAL — Node prompts must NOT reference specific tool names.\n")
	sb.WriteString("   - The node agent discovers its tools from the tools array, NOT from the prompt.\n")
	sb.WriteString("   - WRONG: 'Use shopify_list_products to get products, then use shopify_delete_product...'\n")
	sb.WriteString("   - RIGHT: 'List all products in the store, identify pet-related test products, and delete them.'\n")
	sb.WriteString("   - Describe WHAT to do, not WHICH tool to call. The agent will pick the right tool.\n")
	sb.WriteString("6. Explain your reasoning clearly.\n\n")
	sb.WriteString("IMPORTANT: Prefer producing execution_nodes over creating more sub-goals.\n")
	sb.WriteString("Only decompose into children if the task genuinely requires multiple independent work streams.\n")
	sb.WriteString("Most tasks should be handled with a 1-5 node execution graph, not more tree mutations.\n")

	return sb.String()
}

// buildStrategicPrompt constructs the prompt for strategic (full-tree) planning.
func buildStrategicPrompt(mission string, tree []*Objective, toolCatalog string, memory string) string {
	var sb strings.Builder

	sb.WriteString("You are a strategic planner for an autonomous objective management system.\n")
	sb.WriteString("Your job is to create or restructure the objective tree for a high-level mission.\n\n")

	fmt.Fprintf(&sb, "## Mission\n%s\n\n", mission)

	if len(tree) > 0 {
		sb.WriteString("## Current Objective Tree\n")
		for _, obj := range tree {
			indent := strings.Repeat("  ", obj.Depth)
			fmt.Fprintf(&sb, "%s- [%s] %s (status: %s)\n", indent, obj.ID, obj.Title, obj.Status)
		}
		sb.WriteString("\n")
	}

	if toolCatalog != "" {
		sb.WriteString("## Available Tools\n")
		sb.WriteString(toolCatalog)
		sb.WriteString("\n")
	}

	if memory != "" {
		sb.WriteString("## Relevant Memory\n")
		sb.WriteString(memory)
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Decompose the mission into a hierarchy: Mission → Objectives → Goals → Tasks.\n")
	sb.WriteString("2. Produce tree mutations to build the objective tree.\n")
	sb.WriteString("   - The mission root already exists in the tree — DO NOT create a new root.\n")
	sb.WriteString("   - Use 'add' mutations to create children under the existing root.\n")
	sb.WriteString("   - Set parent_id to the root's ID from the Current Objective Tree above.\n")
	sb.WriteString("   - For deeper children, set parent_id to the title of a previously-added mutation in your list.\n")
	sb.WriteString("     The system resolves title references to IDs automatically.\n")
	sb.WriteString("3. Set 'tool_allowlist' on each objective to scope which tools its subtree can use.\n")
	sb.WriteString("   - Use EXACT tool names from the Available Tools list above.\n")
	sb.WriteString("   - Objectives needing research: ['web_search', 'read_webpage'].\n")
	sb.WriteString("   - Objectives needing API interaction: ['http_request', 'read_webpage'].\n")
	sb.WriteString("   - Objectives needing all web access: list all available tools.\n")
	sb.WriteString("4. Do NOT produce execution_nodes for strategic planning — only tree structure.\n")
	sb.WriteString("5. Set priorities (lower number = higher priority) to indicate execution order among siblings.\n")
	sb.WriteString("6. Keep the tree manageable — typically 3-5 objectives under a mission, 2-4 goals per objective.\n")
	sb.WriteString("7. Leaf goals should be concrete enough that a tactical planner can decompose them into tasks.\n")
	sb.WriteString("8. IMPORTANT — If the mission is too vague, ambiguous, or lacks critical details needed for\n")
	sb.WriteString("   effective decomposition, return 'clarifying_questions' instead of mutations.\n")
	sb.WriteString("   - Ask 2-5 specific, actionable questions that would make the mission concrete.\n")
	sb.WriteString("   - Examples of vague missions: 'Make money online', 'Build something cool', 'Run a business'.\n")
	sb.WriteString("   - Examples of concrete missions: 'Build a Shopify dropshipping store selling pet accessories targeting US customers with $500 budget'.\n")
	sb.WriteString("   - If you have questions, return ONLY clarifying_questions and reasoning — no mutations.\n")
	sb.WriteString("   - If the mission is clear enough to act on, proceed with mutations and do NOT ask questions.\n")

	return sb.String()
}

// buildReviewPrompt constructs a prompt that asks the LLM to evaluate whether
// a mission is truly complete or needs additional work.
func buildReviewPrompt(mission string, tree []*Objective, toolCatalog string, memory string) string {
	var sb strings.Builder

	sb.WriteString("You are reviewing whether a mission has been fully accomplished.\n")
	sb.WriteString("Based on the completed work below, decide if the mission is DONE or needs MORE WORK.\n\n")

	fmt.Fprintf(&sb, "## Mission\n%s\n\n", mission)

	sb.WriteString("## Completed Objective Tree\n")
	for _, obj := range tree {
		indent := strings.Repeat("  ", obj.Depth)
		fmt.Fprintf(&sb, "%s- [%s] %s (id: %s, status: %s)\n", indent, obj.ID, obj.Title, obj.ID, obj.Status)
		if obj.LastResult != "" {
			fmt.Fprintf(&sb, "%s  Result: %s\n", indent, truncateStr(obj.LastResult, 200))
		}
	}
	sb.WriteString("\n")

	if toolCatalog != "" {
		sb.WriteString("## Available Tools\n")
		sb.WriteString(toolCatalog)
		sb.WriteString("\n")
	}

	if memory != "" {
		sb.WriteString("## Context\n")
		sb.WriteString(memory)
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("Evaluate the mission status. Respond with:\n")
	sb.WriteString("- reasoning: Your analysis of what has been accomplished and what gaps remain.\n")
	sb.WriteString("- If the mission is COMPLETE (all major goals achieved), return empty mutations.\n")
	sb.WriteString("- If MORE WORK is needed, return mutations to add new objectives under the mission root.\n")
	sb.WriteString("  These should cover only the REMAINING work — do not repeat completed objectives.\n")
	sb.WriteString("  Use the root's UUID (shown in the tree) as parent_id for new top-level children.\n")
	sb.WriteString("  For deeper children, use the title of a previously-added mutation as parent_id.\n")
	sb.WriteString("  Structure new work as you would in strategic planning: 2-4 high-level objectives with leaf tasks.\n")
	sb.WriteString("- Do NOT produce execution_nodes — only tree mutations for remaining work.\n")

	return sb.String()
}
