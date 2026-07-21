package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SaveSkillInput defines the input for saving a skill.
type SaveSkillInput struct {
	Name         string            `json:"name" description:"Snake_case identifier for the skill" required:"true"`
	Description  string            `json:"description" description:"Natural language description of what the skill does" required:"true"`
	InputSchema  map[string]string `json:"input_schema" description:"Map of argument names to their types (string, int, bool, float, list)" required:"true"`
	Code         string            `json:"code" description:"Python source code defining a run() function that accepts the input_schema arguments" required:"true"`
	Dependencies []string          `json:"dependencies" description:"Optional list of pip packages required" required:"false"`
}

// ExecuteSkillInput defines the input for executing a skill.
type ExecuteSkillInput struct {
	SkillName string         `json:"skill_name" description:"Name of the skill to execute" required:"true"`
	Arguments map[string]any `json:"arguments" description:"Arguments to pass to the skill, matching its input_schema" required:"true"`
}

// ListSkillsInput defines the input for listing skills.
type ListSkillsInput struct {
	// No input required
}

// GetSkillInput defines the input for retrieving a skill.
type GetSkillInput struct {
	SkillName string `json:"skill_name" description:"Name of the skill to retrieve" required:"true"`
}

// DeleteSkillInput defines the input for deleting a skill.
type DeleteSkillInput struct {
	SkillName string `json:"skill_name" description:"Name of the skill to delete" required:"true"`
}

// TestSkillInput defines the input for testing a skill before saving.
type TestSkillInput struct {
	Code         string         `json:"code" description:"Python source code defining a run() function to test" required:"true"`
	Arguments    map[string]any `json:"arguments" description:"Test arguments to pass to the run() function" required:"true"`
	Dependencies []string       `json:"dependencies" description:"Optional list of pip packages required" required:"false"`
}

// SkillsTools provides skill management tools.
type SkillsTools struct {
	pythonTools *PythonTools
	service     *data.AgentService
}

// NewSkillsTools creates a new SkillsTools instance.
func NewSkillsTools(pythonTools *PythonTools, service *data.AgentService) *SkillsTools {
	return &SkillsTools{
		pythonTools: pythonTools,
		service:     service,
	}
}

// SaveSkillTool saves a new skill.
func (s *SkillsTools) SaveSkillTool(ctx context.Context, input SaveSkillInput) (*loop.ToolResult, error) {
	if input.Name == "" {
		return loop.NewErrorResult("name is required"), nil
	}
	if input.Description == "" {
		return loop.NewErrorResult("description is required"), nil
	}
	if input.Code == "" {
		return loop.NewErrorResult("code is required"), nil
	}
	if input.InputSchema == nil {
		input.InputSchema = make(map[string]string)
	}

	// Validate name is snake_case
	if strings.Contains(input.Name, " ") || strings.Contains(input.Name, "-") {
		return loop.NewErrorResult("name must be snake_case (use underscores, no spaces or hyphens)"), nil
	}

	skill := &data.Skill{
		Name:         input.Name,
		Description:  input.Description,
		InputSchema:  input.InputSchema,
		Code:         input.Code,
		Dependencies: input.Dependencies,
	}

	isUpdate, err := s.service.SaveSkill(ctx, skill)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to save skill: %v", err)), nil
	}

	action := "created"
	if isUpdate {
		action = "updated"
	}

	return loop.NewSuccessResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Skill '%s' %s successfully", input.Name, action),
		"skill": map[string]any{
			"name":         input.Name,
			"description":  input.Description,
			"input_schema": input.InputSchema,
			"dependencies": input.Dependencies,
		},
	}), nil
}

// ExecuteSkillTool executes a saved skill.
func (s *SkillsTools) ExecuteSkillTool(ctx context.Context, input ExecuteSkillInput) (*loop.ToolResult, error) {
	if input.SkillName == "" {
		return loop.NewErrorResult("skill_name is required"), nil
	}

	skill, err := s.service.GetSkill(ctx, input.SkillName)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to load skill: %v", err)), nil
	}
	if skill == nil {
		return loop.NewErrorResult(fmt.Sprintf("skill '%s' not found", input.SkillName)), nil
	}

	return s.executeSkill(ctx, skill.Code, skill.InputSchema, skill.Dependencies, input.Arguments)
}

func (s *SkillsTools) executeSkill(ctx context.Context, code string, inputSchema map[string]string, dependencies []string, arguments map[string]any) (*loop.ToolResult, error) {
	// Validate arguments against schema
	if arguments == nil {
		arguments = make(map[string]any)
	}

	for argName := range inputSchema {
		if _, exists := arguments[argName]; !exists {
			return loop.NewErrorResult(fmt.Sprintf("missing required argument: %s", argName)), nil
		}
	}

	// Build the Python script
	var scriptBuilder strings.Builder

	// Install dependencies if any
	if len(dependencies) > 0 {
		scriptBuilder.WriteString("# --- DEPENDENCY INSTALLATION ---\n")
		scriptBuilder.WriteString("import subprocess\n")
		scriptBuilder.WriteString("import sys\n\n")
		scriptBuilder.WriteString("_packages = ")
		depsLiteral, _ := toPythonLiteral(toAnySlice(dependencies))
		scriptBuilder.WriteString(depsLiteral)
		scriptBuilder.WriteString("\n")
		scriptBuilder.WriteString(`for _pkg in _packages:
    try:
        subprocess.check_call([sys.executable, "-m", "pip", "install", "-q", _pkg],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except Exception as e:
        print(f"Warning: Could not install {_pkg}: {e}")
`)
		scriptBuilder.WriteString("# --- END DEPENDENCY INSTALLATION ---\n\n")
	}

	// Add the skill code (which should define a run() function)
	scriptBuilder.WriteString(code)
	scriptBuilder.WriteString("\n\n")

	// Inject arguments as a dictionary and call with **kwargs
	scriptBuilder.WriteString("# Injected arguments\n")
	argsLiteral, err := toPythonLiteral(arguments)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to convert arguments: %v", err)), nil
	}
	scriptBuilder.WriteString(fmt.Sprintf("_args = %s\n", argsLiteral))

	// Call run() with keyword arguments and capture return value as JSON
	scriptBuilder.WriteString(`
# Execute with keyword arguments and capture return value
import json as _json
_result = run(**_args)
if _result is not None:
    try:
        print(_json.dumps(_result))
    except (TypeError, ValueError):
        print(_result)
`)

	// Execute using PythonTools
	result, err := s.pythonTools.RunPythonTool(ctx, RunPythonInput{
		Code:    scriptBuilder.String(),
		Timeout: 60, // Skills get a longer timeout
	})

	return result, err
}

// ListSkillsTool lists all saved skills.
func (s *SkillsTools) ListSkillsTool(ctx context.Context, input ListSkillsInput) (*loop.ToolResult, error) {
	skills, err := s.service.GetAllSkills(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to load skills: %v", err)), nil
	}

	if len(skills) == 0 {
		return loop.NewSuccessResult(map[string]any{
			"skills": []any{},
			"count":  0,
		}), nil
	}

	skillList := make([]map[string]any, 0, len(skills))
	for _, skill := range skills {
		skillList = append(skillList, map[string]any{
			"name":         skill.Name,
			"description":  skill.Description,
			"input_schema": skill.InputSchema,
			"dependencies": skill.Dependencies,
		})
	}

	return loop.NewSuccessResult(map[string]any{
		"skills": skillList,
		"count":  len(skillList),
	}), nil
}

// GetSkillTool retrieves a specific skill's details including code.
func (s *SkillsTools) GetSkillTool(ctx context.Context, input GetSkillInput) (*loop.ToolResult, error) {
	if input.SkillName == "" {
		return loop.NewErrorResult("skill_name is required"), nil
	}

	skill, err := s.service.GetSkill(ctx, input.SkillName)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to load skill: %v", err)), nil
	}
	if skill == nil {
		return loop.NewErrorResult(fmt.Sprintf("skill '%s' not found", input.SkillName)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"name":         skill.Name,
		"description":  skill.Description,
		"input_schema": skill.InputSchema,
		"code":         skill.Code,
		"dependencies": skill.Dependencies,
	}), nil
}

// DeleteSkillTool deletes a skill.
func (s *SkillsTools) DeleteSkillTool(ctx context.Context, input DeleteSkillInput) (*loop.ToolResult, error) {
	if input.SkillName == "" {
		return loop.NewErrorResult("skill_name is required"), nil
	}

	if err := s.service.DeleteSkill(ctx, input.SkillName); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to delete skill: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Skill '%s' deleted successfully", input.SkillName),
	}), nil
}

// TestSkillTool tests skill code without saving it.
func (s *SkillsTools) TestSkillTool(ctx context.Context, input TestSkillInput) (*loop.ToolResult, error) {
	if input.Code == "" {
		return loop.NewErrorResult("code is required"), nil
	}
	if input.Arguments == nil {
		input.Arguments = make(map[string]any)
	}

	// Build the Python script (same logic as executeSkill)
	var scriptBuilder strings.Builder

	// Install dependencies if any
	if len(input.Dependencies) > 0 {
		scriptBuilder.WriteString("# --- DEPENDENCY INSTALLATION ---\n")
		scriptBuilder.WriteString("import subprocess\n")
		scriptBuilder.WriteString("import sys\n\n")
		scriptBuilder.WriteString("_packages = ")
		depsLiteral, _ := toPythonLiteral(toAnySlice(input.Dependencies))
		scriptBuilder.WriteString(depsLiteral)
		scriptBuilder.WriteString("\n")
		scriptBuilder.WriteString(`for _pkg in _packages:
    try:
        subprocess.check_call([sys.executable, "-m", "pip", "install", "-q", _pkg],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except Exception as e:
        print(f"Warning: Could not install {_pkg}: {e}")
`)
		scriptBuilder.WriteString("# --- END DEPENDENCY INSTALLATION ---\n\n")
	}

	// Add the skill code
	scriptBuilder.WriteString(input.Code)
	scriptBuilder.WriteString("\n\n")

	// Inject arguments as kwargs
	scriptBuilder.WriteString("# Injected arguments\n")
	argsLiteral, err := toPythonLiteral(input.Arguments)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to convert arguments: %v", err)), nil
	}
	scriptBuilder.WriteString(fmt.Sprintf("_args = %s\n", argsLiteral))

	// Call run() with keyword arguments and capture return value
	scriptBuilder.WriteString(`
# Execute with keyword arguments and capture return value
import json as _json
_result = run(**_args)
if _result is not None:
    try:
        print(_json.dumps(_result))
    except (TypeError, ValueError):
        print(_result)
`)

	// Execute using PythonTools
	result, err := s.pythonTools.RunPythonTool(ctx, RunPythonInput{
		Code:    scriptBuilder.String(),
		Timeout: 60,
	})

	return result, err
}

// toAnySlice converts a []string to []any for toPythonLiteral.
func toAnySlice(strs []string) []any {
	result := make([]any, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}

// toPythonLiteral converts a Go value to a Python literal string.
func toPythonLiteral(value any) (string, error) {
	switch v := value.(type) {
	case string:
		// Escape quotes and backslashes
		escaped := strings.ReplaceAll(v, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		escaped = strings.ReplaceAll(escaped, "\r", "\\r")
		escaped = strings.ReplaceAll(escaped, "\t", "\\t")
		return fmt.Sprintf("\"%s\"", escaped), nil
	case bool:
		if v {
			return "True", nil
		}
		return "False", nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case []any:
		elements := make([]string, len(v))
		for i, elem := range v {
			elemStr, err := toPythonLiteral(elem)
			if err != nil {
				return "", err
			}
			elements[i] = elemStr
		}
		return fmt.Sprintf("[%s]", strings.Join(elements, ", ")), nil
	case map[string]any:
		pairs := make([]string, 0, len(v))
		for k, val := range v {
			keyStr, _ := toPythonLiteral(k)
			valStr, err := toPythonLiteral(val)
			if err != nil {
				return "", err
			}
			pairs = append(pairs, fmt.Sprintf("%s: %s", keyStr, valStr))
		}
		return fmt.Sprintf("{%s}", strings.Join(pairs, ", ")), nil
	case nil:
		return "None", nil
	default:
		// Try JSON encoding as fallback
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("unsupported type: %T", v)
		}
		return string(jsonBytes), nil
	}
}

// DescribeTool implements ToolProvider for tool descriptions.
func (s *SkillsTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"save_skill": `Save a reusable Python skill for later execution. Requirements:
- Define a run() function with KEYWORD arguments matching input_schema names exactly
- Example: if input_schema has {"url": "string", "limit": "int"}, define: def run(url, limit):
- To return structured data, simply return a dict/list from run() - it will be JSON serialized automatically
- You can also use print() for output, but return values are preferred for structured data
- List pip dependencies in the dependencies array - they are auto-installed before execution
- Skills persist in the database`,

		"execute_skill": `Execute a previously saved skill. Key behaviors:
- Arguments are passed to run() as **kwargs (keyword arguments), so parameter ORDER DOES NOT MATTER
- Only the argument NAMES must match the input_schema - {"limit": 5, "url": "..."} works the same as {"url": "...", "limit": 5}
- If run() returns a value (dict, list, string, number), it is automatically JSON-serialized to stdout
- Dependencies from the skill definition are pip-installed before execution
- Container has internet access for web requests`,

		"list_skills":  "List all saved skills with their names, descriptions, and input schemas. Use this to discover available skills before executing them.",
		"get_skill":    "Retrieve complete details of a skill including its source code. Use this to inspect, debug, or understand a skill's implementation before executing or modifying it.",
		"delete_skill": "Delete a saved skill by name. This permanently removes the skill.",

		"test_skill": `Test skill code WITHOUT saving it. Use this to validate code works correctly before saving.
- Provide the code, test arguments, and optional dependencies
- Runs in the same Docker sandbox as execute_skill
- Returns stdout/stderr and exit code so you can verify correctness
- If the test passes, use save_skill to persist it
- This keeps the skill library free of broken skills`,
	}
	return descriptions[name]
}
