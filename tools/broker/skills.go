package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SkillsTools proxies skill CRUD through the broker API.
// Execute and test run Python locally in the container.
type SkillsTools struct {
	client      *Client
	pythonTools *tools.PythonTools
}

func NewSkillsTools(client *Client, pythonTools *tools.PythonTools) *SkillsTools {
	return &SkillsTools{client: client, pythonTools: pythonTools}
}

func (s *SkillsTools) SaveSkillTool(ctx context.Context, input tools.SaveSkillInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "save_skill", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SkillsTools) ListSkillsTool(ctx context.Context, input tools.ListSkillsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "list_skills", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SkillsTools) GetSkillTool(ctx context.Context, input tools.GetSkillInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "get_skill", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SkillsTools) DeleteSkillTool(ctx context.Context, input tools.DeleteSkillInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "delete_skill", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// ExecuteSkillTool loads the skill definition from the broker, then runs Python locally.
func (s *SkillsTools) ExecuteSkillTool(ctx context.Context, input tools.ExecuteSkillInput) (*loop.ToolResult, error) {
	if input.SkillName == "" {
		return loop.NewErrorResult("skill_name is required"), nil
	}

	// Load skill from broker
	result, err := s.client.CallTool(ctx, "get_skill", tools.GetSkillInput{SkillName: input.SkillName})
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	code, _ := result["code"].(string)
	if code == "" {
		return loop.NewErrorResult(fmt.Sprintf("skill '%s' not found or has no code", input.SkillName)), nil
	}

	// Parse input_schema
	inputSchema := make(map[string]string)
	if schemaRaw, ok := result["input_schema"]; ok {
		if schemaMap, ok := schemaRaw.(map[string]any); ok {
			for k, v := range schemaMap {
				inputSchema[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Parse dependencies
	var deps []string
	if depsRaw, ok := result["dependencies"]; ok {
		if depsArr, ok := depsRaw.([]any); ok {
			for _, d := range depsArr {
				deps = append(deps, fmt.Sprintf("%v", d))
			}
		}
	}

	// Build and execute Python script (same logic as tools.SkillsTools.executeSkill)
	arguments := input.Arguments
	if arguments == nil {
		arguments = make(map[string]any)
	}
	for argName := range inputSchema {
		if _, exists := arguments[argName]; !exists {
			return loop.NewErrorResult(fmt.Sprintf("missing required argument: %s", argName)), nil
		}
	}

	script := buildSkillScript(code, deps, arguments)
	return s.pythonTools.RunPythonTool(ctx, tools.RunPythonInput{
		Code:    script,
		Timeout: 60,
	})
}

// TestSkillTool tests skill code locally without saving.
func (s *SkillsTools) TestSkillTool(ctx context.Context, input tools.TestSkillInput) (*loop.ToolResult, error) {
	if input.Code == "" {
		return loop.NewErrorResult("code is required"), nil
	}
	if input.Arguments == nil {
		input.Arguments = make(map[string]any)
	}

	script := buildSkillScript(input.Code, input.Dependencies, input.Arguments)
	return s.pythonTools.RunPythonTool(ctx, tools.RunPythonInput{
		Code:    script,
		Timeout: 60,
	})
}

func (s *SkillsTools) DescribeTool(name string) string {
	return tools.NewSkillsTools(nil, nil).DescribeTool(name)
}

// buildSkillScript builds the Python script for skill execution.
func buildSkillScript(code string, dependencies []string, arguments map[string]any) string {
	var sb strings.Builder

	if len(dependencies) > 0 {
		sb.WriteString("import subprocess\nimport sys\n\n")
		depsJSON, _ := json.Marshal(dependencies)
		sb.WriteString(fmt.Sprintf("_packages = %s\n", string(depsJSON)))
		sb.WriteString(`for _pkg in _packages:
    try:
        subprocess.check_call([sys.executable, "-m", "pip", "install", "-q", _pkg],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except Exception as e:
        print(f"Warning: Could not install {_pkg}: {e}")
`)
	}

	sb.WriteString(code)
	sb.WriteString("\n\n")

	argsJSON, _ := json.Marshal(arguments)
	sb.WriteString(fmt.Sprintf("import json as _json\n_args = _json.loads('%s')\n", string(argsJSON)))
	sb.WriteString(`_result = run(**_args)
if _result is not None:
    try:
        print(_json.dumps(_result))
    except (TypeError, ValueError):
        print(_result)
`)
	return sb.String()
}
