package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

func handleRecurringListCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printRecurringTasks()
	return cmdContinue
}

func handleAddRecurringCommand(cm data.CommandMessage, _ commandContext) commandResult {
	if globalReadline != nil {
		inlineInterval := cmdArg(cm, "interval")
		inlineDesc := cmdArg(cm, "description")
		addRecurringTask(globalReadline, inlineInterval, inlineDesc)
	} else {
		fmt.Println(color.RedString("Interactive input not available"))
	}
	return cmdContinue
}

func handleDeleteRecurringCommand(cm data.CommandMessage, _ commandContext) commandResult {
	taskID := cmdArg(cm, "id")
	if taskID != "" {
		deleteRecurringTaskByID(taskID)
	} else if globalReadline != nil {
		deleteRecurringTask(globalReadline)
	} else {
		output.Error("Usage: /deleterecurring <task_id>")
	}
	return cmdContinue
}

// printRecurringTasks prints the list of recurring tasks.
func printRecurringTasks() {
	ctx := context.Background()
	taskList, err := getRecurringTasksData(ctx)
	if err != nil {
		output.Error("Error loading recurring tasks: %v", err)
		return
	}
	if len(taskList) == 0 {
		output.System("No recurring tasks.")
		return
	}

	output.System("Recurring Tasks (%d):", len(taskList))

	for i, t := range taskList {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}
		taskID, _ := task["id"].(string)
		description, _ := task["description"].(string)
		intervalMinutes, _ := task["interval_minutes"].(float64)
		lastCreatedAtStr, _ := task["last_created_at"].(string)

		interval := tools.FormatIntervalForDisplay(int(intervalMinutes))
		var lastCreatedAt time.Time
		if t, err := time.Parse(time.RFC3339, lastCreatedAtStr); err == nil {
			lastCreatedAt = t
		}
		output.System("  %d. %s: %s (every %s)", i+1, taskID, description, interval)
		output.System("     last created: %s", lastCreatedAt.Format("Jan 2, 2006 at 3:04 PM"))
	}
}

// parseIntervalToMinutes parses an interval string like "50m", "3h", "4d", "1h30m" to minutes.
func parseIntervalToMinutes(input string) (int, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return 0, fmt.Errorf("interval cannot be empty")
	}

	// Parse components like "4d", "3h", "50m", "1h30m", "2d12h"
	totalMinutes := 0
	remaining := input

	// Parse days
	if idx := strings.Index(remaining, "d"); idx != -1 {
		var days int
		if _, err := fmt.Sscanf(remaining[:idx], "%d", &days); err != nil {
			return 0, fmt.Errorf("invalid days in interval")
		}
		totalMinutes += days * 24 * 60
		remaining = remaining[idx+1:]
	}

	// Parse hours
	if idx := strings.Index(remaining, "h"); idx != -1 {
		var hours int
		if _, err := fmt.Sscanf(remaining[:idx], "%d", &hours); err != nil {
			return 0, fmt.Errorf("invalid hours in interval")
		}
		totalMinutes += hours * 60
		remaining = remaining[idx+1:]
	}

	// Parse minutes
	if idx := strings.Index(remaining, "m"); idx != -1 {
		var mins int
		if _, err := fmt.Sscanf(remaining[:idx], "%d", &mins); err != nil {
			return 0, fmt.Errorf("invalid minutes in interval")
		}
		totalMinutes += mins
	} else if remaining != "" {
		// Remaining string has no unit, treat as minutes if it's a number
		var mins int
		if _, err := fmt.Sscanf(remaining, "%d", &mins); err == nil {
			totalMinutes += mins
		} else if remaining != "" {
			return 0, fmt.Errorf("invalid interval format: %s", input)
		}
	}

	if totalMinutes <= 0 {
		return 0, fmt.Errorf("interval must be positive")
	}
	return totalMinutes, nil
}

// looksLikeInterval checks if a string looks like a time interval (e.g., 30m, 2h, 1d, 1h30m).
func looksLikeInterval(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	// Must contain at least one digit and end with d, h, or m
	hasDigit := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c != 'd' && c != 'h' && c != 'm' {
			return false
		}
	}
	last := s[len(s)-1]
	return hasDigit && (last == 'd' || last == 'h' || last == 'm')
}

// addRecurringTask handles the /addrecurring command.
func addRecurringTask(rl *readline.Instance, inlineInterval, inlineDesc string) {
	var description string
	var intervalMinutes int

	switch {
	case inlineInterval != "" && inlineDesc != "":
		// Both provided inline: /addrecurring 30m check email
		mins, err := parseIntervalToMinutes(inlineInterval)
		if err != nil {
			fmt.Println(color.RedString("Invalid interval %q: %v", inlineInterval, err))
			fmt.Println(color.HiBlackString("  Usage: /addrecurring <interval> <description>"))
			fmt.Println(color.HiBlackString("  e.g.,  /addrecurring 30m check email"))
			return
		}
		description = inlineDesc
		intervalMinutes = mins

	case inlineInterval != "" && inlineDesc == "":
		// Only interval provided: /addrecurring 30m — prompt for description
		mins, err := parseIntervalToMinutes(inlineInterval)
		if err != nil {
			fmt.Println(color.RedString("Invalid interval %q: %v", inlineInterval, err))
			fmt.Println(color.HiBlackString("  Usage: /addrecurring <interval> <description>"))
			fmt.Println(color.HiBlackString("  e.g.,  /addrecurring 30m check email"))
			return
		}
		rl.SetPrompt(color.CyanString("Task description: "))
		desc, err := rl.Readline()
		rl.SetPrompt("")
		if err != nil || strings.TrimSpace(desc) == "" {
			fmt.Println(color.YellowString("Cancelled."))
			return
		}
		description = strings.TrimSpace(desc)
		intervalMinutes = mins

	case inlineDesc != "":
		// Only description provided: /addrecurring check email — prompt for interval
		rl.SetPrompt(color.CyanString("Recurrence interval (e.g., 30m, 2h, 1d): "))
		intervalStr, err := rl.Readline()
		rl.SetPrompt("")
		if err != nil || strings.TrimSpace(intervalStr) == "" {
			fmt.Println(color.YellowString("Cancelled."))
			return
		}
		mins, err := parseIntervalToMinutes(intervalStr)
		if err != nil {
			fmt.Println(color.RedString("Invalid interval: %v", err))
			return
		}
		description = inlineDesc
		intervalMinutes = mins

	default:
		// Nothing provided: single prompt for "interval description"
		fmt.Println(color.HiBlackString("  Enter as: <interval> <description>"))
		fmt.Println(color.HiBlackString("  e.g.,     30m check email"))
		rl.SetPrompt(color.CyanString("New recurring task: "))
		line, err := rl.Readline()
		rl.SetPrompt("")
		if err != nil || strings.TrimSpace(line) == "" {
			fmt.Println(color.YellowString("Cancelled."))
			return
		}
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) < 2 {
			fmt.Println(color.RedString("Need both interval and description."))
			fmt.Println(color.HiBlackString("  e.g., 30m check email"))
			return
		}
		mins, err := parseIntervalToMinutes(parts[0])
		if err != nil {
			fmt.Println(color.RedString("Invalid interval %q: %v", parts[0], err))
			return
		}
		description = strings.Join(parts[1:], " ")
		intervalMinutes = mins
	}

	ctx := context.Background()
	result, err := addRecurringTaskData(ctx, description, intervalMinutes)
	if err != nil {
		output.Error("Error adding recurring task: %v", err)
		return
	}

	returnedInterval, _ := result["interval_minutes"].(float64)
	interval := tools.FormatIntervalForDisplay(int(returnedInterval))
	output.SystemSuccess("Recurring task added: %s (every %s)", description, interval)
}

// deleteRecurringTask handles the /deleterecurring command with interactive selection.
func deleteRecurringTask(rl *readline.Instance) {
	ctx := context.Background()
	taskList, err := getRecurringTasksData(ctx)
	if err != nil {
		output.Error("Error loading recurring tasks: %v", err)
		return
	}
	if len(taskList) == 0 {
		output.SystemWarning("No recurring tasks to delete.")
		return
	}

	fmt.Println(color.YellowString("Select a recurring task to delete:"))
	fmt.Println()

	// Build a list of task info for display and deletion
	type taskInfo struct {
		ID          string
		Description string
		Interval    string
	}
	var tasks []taskInfo
	for _, t := range taskList {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}
		taskID, _ := task["id"].(string)
		description, _ := task["description"].(string)
		intervalMinutes, _ := task["interval_minutes"].(float64)
		interval := tools.FormatIntervalForDisplay(int(intervalMinutes))
		tasks = append(tasks, taskInfo{ID: taskID, Description: description, Interval: interval})
	}

	for i, task := range tasks {
		fmt.Printf("  %d. %s (every %s)\n", i+1, task.Description, task.Interval)
	}
	fmt.Println()

	rl.SetPrompt(color.CyanString("Enter number to delete (or 0 to cancel): "))
	choice, err := rl.Readline()
	rl.SetPrompt("")
	if err != nil {
		fmt.Println(color.YellowString("Cancelled."))
		return
	}

	var num int
	if _, err := fmt.Sscanf(strings.TrimSpace(choice), "%d", &num); err != nil || num < 0 || num > len(tasks) {
		fmt.Println(color.YellowString("Invalid selection."))
		return
	}

	if num == 0 {
		fmt.Println(color.YellowString("Cancelled."))
		return
	}

	task := tasks[num-1]
	_, err = deleteRecurringTaskData(ctx, task.ID)
	if err != nil {
		output.Error("Error deleting recurring task: %v", err)
		return
	}

	output.SystemSuccess("Deleted recurring task: %s", task.Description)
}

// deleteRecurringTaskByID deletes a recurring task by its ID directly.
func deleteRecurringTaskByID(taskID string) {
	ctx := context.Background()
	result, err := deleteRecurringTaskData(ctx, taskID)
	if err != nil {
		output.Error("Error deleting recurring task: %v", err)
		return
	}
	// Show description if available, otherwise show ID
	if desc, ok := result["description"].(string); ok && desc != "" {
		output.SystemSuccess("Deleted recurring task: %s", desc)
	} else {
		output.SystemSuccess("Deleted recurring task: %s", taskID)
	}
}
