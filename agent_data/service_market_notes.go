package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

const maxMarketNoteLength = 2000

// MarketNoteMetadata stores structured thesis details alongside note content.
type MarketNoteMetadata struct {
	Kind                 string    `json:"kind,omitempty"`
	Status               string    `json:"status,omitempty"`
	Action               string    `json:"action,omitempty"`
	Side                 string    `json:"side,omitempty"`
	Question             string    `json:"question,omitempty"`
	Reasoning            string    `json:"reasoning,omitempty"`
	Invalidation         string    `json:"invalidation,omitempty"`
	ResolutionDate       string    `json:"resolution_date,omitempty"`
	MarketFingerprint    string    `json:"market_fingerprint,omitempty"`
	ThesisHash           string    `json:"thesis_hash,omitempty"`
	SourceHashes         []string  `json:"source_hashes,omitempty"`
	EstimatedProbability *float64  `json:"estimated_probability,omitempty"`
	Confidence           *float64  `json:"confidence,omitempty"`
	CurrentPosition      *float64  `json:"current_position,omitempty"`
	MaxAllowed           *float64  `json:"max_allowed,omitempty"`
	RemainingCapacity    *float64  `json:"remaining_capacity,omitempty"`
	Price                *float64  `json:"price,omitempty"`
	Edge                 *float64  `json:"edge,omitempty"`
	RelativeEdge         *float64  `json:"relative_edge,omitempty"`
	Spread               *float64  `json:"spread,omitempty"`
	MarketProbability    *float64  `json:"market_probability,omitempty"`
	MarketVolume         *float64  `json:"market_volume,omitempty"`
	MarketVolume24hr     *float64  `json:"market_volume_24hr,omitempty"`
	Liquidity            *float64  `json:"liquidity,omitempty"`
	DaysToEnd            *float64  `json:"days_to_end,omitempty"`
	OrderbookDepth       *float64  `json:"orderbook_depth,omitempty"`
	CapturedAt           time.Time `json:"captured_at,omitempty"`
}

// MarketNoteSummary contains aggregate note metadata for a market.
type MarketNoteSummary struct {
	Count  int
	Latest *MarketNote
}

// AddMarketNote inserts a note for a market scoped to a company.
func AddMarketNote(ctx context.Context, db gowild_data.Database, companyID, agentID, conditionID, content string) (*MarketNote, error) {
	return AddMarketNoteWithMetadata(ctx, db, companyID, agentID, conditionID, content, nil)
}

// AddMarketNoteWithMetadata inserts a note with optional structured metadata.
func AddMarketNoteWithMetadata(ctx context.Context, db gowild_data.Database, companyID, agentID, conditionID, content string, metadata *MarketNoteMetadata) (*MarketNote, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("note content is required")
	}
	if len(content) > maxMarketNoteLength {
		return nil, fmt.Errorf("note content exceeds %d characters", maxMarketNoteLength)
	}
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("condition_id is required")
	}

	metadataJSON := ""
	if normalized := normalizeMarketNoteMetadata(metadata); normalized != nil {
		if b, err := json.Marshal(normalized); err == nil {
			metadataJSON = string(b)
		}
	}

	note := &MarketNote{
		ID:               uuid.New().String(),
		CompanyID:        companyID,
		ConditionID:      conditionID,
		Content:          content,
		MetadataJSON:     metadataJSON,
		CreatedByAgentID: agentID,
		CreatedAt:        time.Now().UTC(),
	}
	if err := db.Table(MarketNote{}).Insert(ctx, note); err != nil {
		return nil, fmt.Errorf("failed to create market note: %w", err)
	}
	return note, nil
}

// ListMarketNotes returns notes for a market scoped to a company, ordered by created_at DESC.
func ListMarketNotes(ctx context.Context, db gowild_data.Database, companyID, conditionID string, limit int) ([]*MarketNote, error) {
	if limit <= 0 {
		limit = 50
	}
	results, err := db.Table(MarketNote{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"company_id": companyID, "condition_id": conditionID},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list market notes: %w", err)
	}
	notes := make([]*MarketNote, 0, len(results))
	for _, r := range results {
		if n, ok := r.(*MarketNote); ok {
			notes = append(notes, n)
		}
	}
	return notes, nil
}

// CountMarketNotes returns the number of notes for a market scoped to a company.
func CountMarketNotes(ctx context.Context, db gowild_data.Database, companyID, conditionID string) (int, error) {
	results, err := db.Table(MarketNote{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID, "condition_id": conditionID},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to count market notes: %w", err)
	}
	return len(results), nil
}

// ListMarketNotesByMarkets returns up to limitPerMarket notes per market for many markets in one query.
func ListMarketNotesByMarkets(ctx context.Context, db gowild_data.Database, companyID string, conditionIDs []string, limitPerMarket int) (map[string][]*MarketNote, error) {
	normalized := uniqueMarketConditionIDs(conditionIDs)
	if len(normalized) == 0 {
		return map[string][]*MarketNote{}, nil
	}
	if limitPerMarket <= 0 {
		limitPerMarket = 50
	}

	values := make([]any, 0, len(normalized))
	for _, conditionID := range normalized {
		values = append(values, conditionID)
	}

	results, err := db.Table(MarketNote{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"company_id": companyID},
		WhereIn:   map[string][]any{"condition_id": values},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load market notes: %w", err)
	}

	notesByMarket := make(map[string][]*MarketNote, len(normalized))
	for _, conditionID := range normalized {
		notesByMarket[conditionID] = nil
	}
	for _, row := range results {
		note, ok := row.(*MarketNote)
		if !ok || note == nil {
			continue
		}
		conditionID := strings.TrimSpace(note.ConditionID)
		if conditionID == "" {
			continue
		}
		current := notesByMarket[conditionID]
		if len(current) >= limitPerMarket {
			continue
		}
		notesByMarket[conditionID] = append(current, note)
	}
	return notesByMarket, nil
}

// GetMarketNoteSummaries returns note counts and latest notes for many markets in one query.
func GetMarketNoteSummaries(ctx context.Context, db gowild_data.Database, companyID string, conditionIDs []string) (map[string]MarketNoteSummary, error) {
	normalized := uniqueMarketConditionIDs(conditionIDs)
	if len(normalized) == 0 {
		return map[string]MarketNoteSummary{}, nil
	}

	values := make([]any, 0, len(normalized))
	for _, conditionID := range normalized {
		values = append(values, conditionID)
	}

	results, err := db.Table(MarketNote{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"company_id": companyID},
		WhereIn:   map[string][]any{"condition_id": values},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load market note summaries: %w", err)
	}

	summaries := make(map[string]MarketNoteSummary, len(normalized))
	for _, conditionID := range normalized {
		summaries[conditionID] = MarketNoteSummary{}
	}
	for _, row := range results {
		note, ok := row.(*MarketNote)
		if !ok || note == nil {
			continue
		}
		conditionID := strings.TrimSpace(note.ConditionID)
		if conditionID == "" {
			continue
		}
		summary := summaries[conditionID]
		summary.Count++
		if summary.Latest == nil {
			summary.Latest = note
		}
		summaries[conditionID] = summary
	}
	return summaries, nil
}

// ParseMarketNoteMetadata decodes structured metadata from a market note when present.
func ParseMarketNoteMetadata(note *MarketNote) *MarketNoteMetadata {
	if note == nil {
		return nil
	}
	return ParseMarketNoteMetadataJSON(note.MetadataJSON)
}

// ParseMarketNoteMetadataJSON decodes structured metadata when present.
func ParseMarketNoteMetadataJSON(raw string) *MarketNoteMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var metadata MarketNoteMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil
	}
	if normalized := normalizeMarketNoteMetadata(&metadata); normalized != nil {
		return normalized
	}
	return nil
}

func uniqueMarketConditionIDs(conditionIDs []string) []string {
	if len(conditionIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(conditionIDs))
	normalized := make([]string, 0, len(conditionIDs))
	for _, conditionID := range conditionIDs {
		conditionID = strings.TrimSpace(conditionID)
		if conditionID == "" {
			continue
		}
		if _, ok := seen[conditionID]; ok {
			continue
		}
		seen[conditionID] = struct{}{}
		normalized = append(normalized, conditionID)
	}
	return normalized
}

func normalizeMarketNoteMetadata(metadata *MarketNoteMetadata) *MarketNoteMetadata {
	if metadata == nil {
		return nil
	}
	copy := *metadata
	copy.Kind = strings.TrimSpace(copy.Kind)
	copy.Status = strings.TrimSpace(copy.Status)
	copy.Action = strings.TrimSpace(copy.Action)
	copy.Side = strings.TrimSpace(copy.Side)
	copy.Question = strings.TrimSpace(copy.Question)
	copy.Reasoning = strings.TrimSpace(copy.Reasoning)
	copy.Invalidation = strings.TrimSpace(copy.Invalidation)
	copy.ResolutionDate = strings.TrimSpace(copy.ResolutionDate)
	copy.MarketFingerprint = strings.TrimSpace(copy.MarketFingerprint)
	copy.ThesisHash = strings.TrimSpace(copy.ThesisHash)
	copy.SourceHashes = normalizeMarketNoteHashes(copy.SourceHashes)
	copy.CapturedAt = copy.CapturedAt.UTC()

	if marketNoteMetadataIsEmpty(copy) {
		return nil
	}
	return &copy
}

func normalizeMarketNoteHashes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func marketNoteMetadataIsEmpty(metadata MarketNoteMetadata) bool {
	return metadata.Kind == "" &&
		metadata.Status == "" &&
		metadata.Action == "" &&
		metadata.Side == "" &&
		metadata.Question == "" &&
		metadata.Reasoning == "" &&
		metadata.Invalidation == "" &&
		metadata.ResolutionDate == "" &&
		metadata.MarketFingerprint == "" &&
		metadata.ThesisHash == "" &&
		len(metadata.SourceHashes) == 0 &&
		metadata.EstimatedProbability == nil &&
		metadata.Confidence == nil &&
		metadata.CurrentPosition == nil &&
		metadata.MaxAllowed == nil &&
		metadata.RemainingCapacity == nil &&
		metadata.Price == nil &&
		metadata.Edge == nil &&
		metadata.RelativeEdge == nil &&
		metadata.Spread == nil &&
		metadata.MarketProbability == nil &&
		metadata.MarketVolume == nil &&
		metadata.MarketVolume24hr == nil &&
		metadata.Liquidity == nil &&
		metadata.DaysToEnd == nil &&
		metadata.OrderbookDepth == nil &&
		metadata.CapturedAt.IsZero()
}
