package pipelinespec

import (
	"encoding/json"
	"fmt"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
)

const (
	RunnerAgent      = "agent"
	RunnerBuiltin    = "builtin"
	RunnerClaudeCode = "claude-code"
	RunnerCodex      = "codex"

	BuiltinPolymarketFindMarkets    = "builtin_polymarket_find_markets"
	BuiltinPolymarketSnapshot       = "builtin_polymarket_snapshot"
	BuiltinPolymarketManagePosition = "builtin_polymarket_manage_position"
)

var builtinMethodAliases = map[string]string{
	BuiltinPolymarketFindMarkets:   BuiltinPolymarketFindMarkets,
	"/polymarket_find_markets":     BuiltinPolymarketFindMarkets,
	"polymarket_find_markets":      BuiltinPolymarketFindMarkets,
	BuiltinPolymarketSnapshot:      BuiltinPolymarketSnapshot,
	"/polymarket_review_positions": BuiltinPolymarketSnapshot,
	"polymarket_review_positions":  BuiltinPolymarketSnapshot,

	BuiltinPolymarketManagePosition: BuiltinPolymarketManagePosition,
	"/polymarket_manage_position":   BuiltinPolymarketManagePosition,
	"polymarket_manage_position":    BuiltinPolymarketManagePosition,
}

var builtinMethodValidator = defaultBuiltinMethodValidator

type Definition struct {
	ID             string
	Name           string
	ScopeMode      string
	ScopeCompanyID string
	Schedule       string
	Steps          []Step
}

type Step struct {
	Runner     string
	OnMethod   string
	OnStatus   string
	FromRole   string
	ToAgentID  string
	ToRole     string
	NextMethod string
	ParamMap   map[string]string
	FanOut     bool
	FanOutKey  string
}

func CloneAll(in []Definition) []Definition {
	out := make([]Definition, len(in))
	for i := range in {
		out[i] = Clone(in[i])
	}
	return out
}

func Clone(in Definition) Definition {
	out := Definition{
		ID:             in.ID,
		Name:           in.Name,
		ScopeMode:      in.ScopeMode,
		ScopeCompanyID: in.ScopeCompanyID,
		Schedule:       in.Schedule,
		Steps:          make([]Step, len(in.Steps)),
	}
	for i := range in.Steps {
		step := in.Steps[i]
		copiedStep := Step{
			Runner:     step.Runner,
			OnMethod:   step.OnMethod,
			OnStatus:   step.OnStatus,
			FromRole:   step.FromRole,
			ToAgentID:  step.ToAgentID,
			ToRole:     step.ToRole,
			NextMethod: step.NextMethod,
			FanOut:     step.FanOut,
			FanOutKey:  step.FanOutKey,
		}
		if len(step.ParamMap) > 0 {
			copiedStep.ParamMap = make(map[string]string, len(step.ParamMap))
			for k, v := range step.ParamMap {
				copiedStep.ParamMap[k] = v
			}
		}
		out.Steps[i] = copiedStep
	}
	return out
}

func Normalize(in Definition) Definition {
	p := Clone(in)
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.ScopeMode = strings.TrimSpace(p.ScopeMode)
	p.ScopeCompanyID = strings.TrimSpace(p.ScopeCompanyID)
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.ScopeMode == "" {
		p.ScopeMode = "global"
	}
	if p.ScopeMode == "global" {
		p.ScopeCompanyID = ""
	}
	for i := range p.Steps {
		step := &p.Steps[i]
		step.Runner = NormalizeRunner(step.Runner)
		step.OnMethod = strings.TrimSpace(step.OnMethod)
		step.OnStatus = strings.TrimSpace(step.OnStatus)
		step.FromRole = strings.TrimSpace(step.FromRole)
		step.ToAgentID = strings.TrimSpace(step.ToAgentID)
		step.ToRole = strings.TrimSpace(step.ToRole)
		if step.Runner == RunnerBuiltin {
			step.NextMethod = NormalizeBuiltinMethod(step.NextMethod)
		} else {
			step.NextMethod = strings.TrimSpace(step.NextMethod)
		}
		step.FanOutKey = strings.TrimSpace(step.FanOutKey)
		if step.ParamMap == nil {
			step.ParamMap = map[string]string{}
		}
	}
	return p
}

func Validate(p Definition) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("pipeline id is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("pipeline name is required")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("pipeline must define at least one step")
	}
	switch p.ScopeMode {
	case "", "global":
	case "company":
		if strings.TrimSpace(p.ScopeCompanyID) == "" {
			return fmt.Errorf("scope_company_id is required when scope_mode=company")
		}
	default:
		return fmt.Errorf("invalid scope_mode %q", p.ScopeMode)
	}
	for i, step := range p.Steps {
		if strings.TrimSpace(step.OnMethod) == "" {
			return fmt.Errorf("step %d: on_method is required", i)
		}
		switch NormalizeRunner(step.Runner) {
		case RunnerAgent:
			if strings.TrimSpace(step.ToRole) == "" && strings.TrimSpace(step.ToAgentID) == "" {
				return fmt.Errorf("step %d: to_agent_id or to_role is required", i)
			}
			if strings.TrimSpace(step.NextMethod) == "" {
				return fmt.Errorf("step %d: next_method is required", i)
			}
		case RunnerBuiltin:
			if strings.TrimSpace(step.NextMethod) == "" {
				return fmt.Errorf("step %d: next_method is required", i)
			}
			if !IsBuiltinMethod(step.NextMethod) {
				return fmt.Errorf("step %d: unknown builtin method %q", i, step.NextMethod)
			}
		case RunnerClaudeCode:
			if err := validateCLIRunnerStep(i, step, RunnerClaudeCode); err != nil {
				return err
			}
		case RunnerCodex:
			if err := validateCLIRunnerStep(i, step, RunnerCodex); err != nil {
				return err
			}
		default:
			return fmt.Errorf("step %d: invalid runner %q", i, step.Runner)
		}
		if step.FanOut && strings.TrimSpace(step.FanOutKey) == "" {
			return fmt.Errorf("step %d: fan_out_key is required when fan_out=true", i)
		}
	}
	return nil
}

func FromDefinition(def data.PipelineDefinition) (Definition, error) {
	var steps []Step
	if err := json.Unmarshal([]byte(def.StepsJSON), &steps); err != nil {
		return Definition{}, fmt.Errorf("invalid steps_json for %s: %w", def.ID, err)
	}
	p := Normalize(Definition{
		ID:             def.ID,
		Name:           def.Name,
		ScopeMode:      def.ScopeMode,
		ScopeCompanyID: def.ScopeCompanyID,
		Schedule:       def.Schedule,
		Steps:          steps,
	})
	if err := Validate(p); err != nil {
		return Definition{}, err
	}
	return p, nil
}

func NormalizeRunner(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", RunnerAgent:
		return RunnerAgent
	case RunnerBuiltin:
		return RunnerBuiltin
	case RunnerClaudeCode, "claude_code", "claudecode":
		return RunnerClaudeCode
	case RunnerCodex, "codex-code", "openai":
		return RunnerCodex
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func NormalizeBuiltinMethod(raw string) string {
	method := strings.TrimSpace(raw)
	if method == "" {
		return ""
	}
	if canonical, ok := builtinMethodAliases[strings.ToLower(method)]; ok {
		return canonical
	}
	return method
}

func IsBuiltinMethod(method string) bool {
	return builtinMethodValidator(NormalizeBuiltinMethod(method))
}

func SetBuiltinMethodValidator(fn func(string) bool) {
	if fn == nil {
		builtinMethodValidator = defaultBuiltinMethodValidator
		return
	}
	builtinMethodValidator = fn
}

func defaultBuiltinMethodValidator(method string) bool {
	_, ok := builtinMethodAliases[strings.ToLower(strings.TrimSpace(method))]
	return ok
}

// validateCLIRunnerStep validates a step that delegates to an external CLI
// runner. These runners accept arbitrary method names resolved at runtime via
// loadPipelineMethodDefinition against DB-stored A2A methods, so method
// existence is intentionally not checked here — only that the method is not a
// reserved builtin name. The runner argument must be one of the Runner*
// constants (RunnerClaudeCode, RunnerCodex); its string value is surfaced
// verbatim in error messages, so callers should always pass a declared
// constant rather than a literal to avoid typos leaking into user-facing text.
func validateCLIRunnerStep(idx int, step Step, runner string) error {
	if strings.TrimSpace(step.NextMethod) == "" {
		return fmt.Errorf("step %d: next_method is required", idx)
	}
	if IsBuiltinMethod(step.NextMethod) {
		return fmt.Errorf("step %d: builtin methods are not supported for %s runner", idx, runner)
	}
	if strings.TrimSpace(step.ToAgentID) == "" {
		return fmt.Errorf("step %d: to_agent_id is required for %s runner", idx, runner)
	}
	return nil
}
