package deepresearch

import (
	"sort"
	"strings"
)

func dedupeObjectives(objectives []Objective) []Objective {
	if len(objectives) == 0 {
		return nil
	}
	merged := make(map[string]Objective, len(objectives))
	for _, objective := range objectives {
		key := strings.TrimSpace(objective.Key)
		if key == "" {
			continue
		}
		objective.Key = key
		objective.Description = strings.TrimSpace(objective.Description)
		current, ok := merged[key]
		if !ok {
			merged[key] = objective
			continue
		}
		if objective.Required {
			current.Required = true
		}
		if current.Description == "" && objective.Description != "" {
			current.Description = objective.Description
		}
		merged[key] = current
	}
	out := make([]Objective, 0, len(merged))
	for _, objective := range merged {
		out = append(out, objective)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Key < out[j].Key
	})
	return out
}
