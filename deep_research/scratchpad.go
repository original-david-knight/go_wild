package deepresearch

import (
	"sort"
	"sync"
)

type scratchpad struct {
	mu       sync.RWMutex
	findings map[string]map[string]Finding
}

func newScratchpad(objectives []Objective) *scratchpad {
	byObjective := make(map[string]map[string]Finding, len(objectives))
	for _, objective := range objectives {
		if objective.Key == "" {
			continue
		}
		byObjective[objective.Key] = make(map[string]Finding)
	}
	return &scratchpad{findings: byObjective}
}

func (s *scratchpad) addMany(findings []Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, finding := range findings {
		objectiveKey := finding.ObjectiveKey
		if objectiveKey == "" || finding.URL == "" {
			continue
		}
		if _, ok := s.findings[objectiveKey]; !ok {
			s.findings[objectiveKey] = map[string]Finding{}
		}
		existing, exists := s.findings[objectiveKey][finding.URL]
		if exists {
			if finding.Score < existing.Score {
				continue
			}
			if finding.Score == existing.Score && len(finding.Excerpt) <= len(existing.Excerpt) {
				continue
			}
		}
		s.findings[objectiveKey][finding.URL] = finding
	}
}

func (s *scratchpad) count(objectiveKey string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.findings[objectiveKey])
}

func (s *scratchpad) findingsFor(objectiveKey string) []Finding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortFindings(s.findings[objectiveKey])
}

func (s *scratchpad) objectiveResults(objectives []Objective, minEvidence int) []ObjectiveResult {
	results := make([]ObjectiveResult, 0, len(objectives))
	for _, objective := range objectives {
		rows := s.findingsFor(objective.Key)
		result := ObjectiveResult{
			Objective:     objective,
			EvidenceCount: len(rows),
			Status:        ObjectiveStatusMissing,
		}
		switch {
		case len(rows) >= minEvidence:
			result.Status = ObjectiveStatusSatisfied
		case len(rows) > 0:
			result.Status = ObjectiveStatusPartial
		default:
			result.Status = ObjectiveStatusMissing
		}
		if len(rows) > 0 {
			best := rows[0]
			result.BestFinding = &best
		}
		results = append(results, result)
	}
	return results
}

func (s *scratchpad) snapshot() (map[string][]Finding, []Source) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	findings := make(map[string][]Finding, len(s.findings))
	sourcesByURL := map[string]Source{}
	for objectiveKey, perURL := range s.findings {
		sorted := sortFindings(perURL)
		if len(sorted) == 0 {
			continue
		}
		findings[objectiveKey] = sorted
		for _, finding := range sorted {
			source, exists := sourcesByURL[finding.URL]
			if !exists || source.BestScore < finding.Score {
				sourcesByURL[finding.URL] = Source{
					URL:         finding.URL,
					Domain:      finding.Domain,
					Title:       finding.Title,
					BestScore:   finding.Score,
					PublishedAt: finding.PublishedAt,
				}
			}
		}
	}

	sources := make([]Source, 0, len(sourcesByURL))
	for _, source := range sourcesByURL {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].BestScore != sources[j].BestScore {
			return sources[i].BestScore > sources[j].BestScore
		}
		return sources[i].URL < sources[j].URL
	})
	return findings, sources
}

func sortFindings(perURL map[string]Finding) []Finding {
	if len(perURL) == 0 {
		return nil
	}
	rows := make([]Finding, 0, len(perURL))
	for _, finding := range perURL {
		rows = append(rows, finding)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		if !rows[i].RetrievedAt.Equal(rows[j].RetrievedAt) {
			return rows[i].RetrievedAt.After(rows[j].RetrievedAt)
		}
		return rows[i].URL < rows[j].URL
	})
	return rows
}
