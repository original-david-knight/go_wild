package data

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

// Supported MarketProperty value types.
const (
	MarketPropertyTypeString   = "string"
	MarketPropertyTypeDatetime = "datetime"
	MarketPropertyTypeBool     = "bool"
	MarketPropertyTypeCurrency = "currency"
)

// SetMarketProperty upserts a typed key-value pair for a market scoped to a company.
// The value is validated against valueType before storage.
func SetMarketProperty(ctx context.Context, db gowild_data.Database, companyID, conditionID, key, value, valueType string) (*MarketProperty, error) {
	companyID = strings.TrimSpace(companyID)
	conditionID = strings.TrimSpace(conditionID)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	valueType = strings.TrimSpace(strings.ToLower(valueType))

	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	if conditionID == "" {
		return nil, fmt.Errorf("condition_id is required")
	}
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if err := validateMarketPropertyValue(value, valueType); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	existing, err := getMarketPropertyRow(ctx, db, companyID, conditionID, key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.Value = value
		existing.ValueType = valueType
		existing.UpdatedAt = now
		if err := db.Table(MarketProperty{}).Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("failed to update market property: %w", err)
		}
		return existing, nil
	}

	prop := &MarketProperty{
		ID:          uuid.New().String(),
		CompanyID:   companyID,
		ConditionID: conditionID,
		Key:         key,
		Value:       value,
		ValueType:   valueType,
		UpdatedAt:   now,
	}
	if err := db.Table(MarketProperty{}).Insert(ctx, prop); err != nil {
		return nil, fmt.Errorf("failed to insert market property: %w", err)
	}
	return prop, nil
}

// getMarketProperty returns a single property by key, or nil if not found.
func getMarketProperty(ctx context.Context, db gowild_data.Database, companyID, conditionID, key string) (*MarketProperty, error) {
	return getMarketPropertyRow(ctx, db, strings.TrimSpace(companyID), strings.TrimSpace(conditionID), strings.TrimSpace(key))
}

// listMarketProperties returns all properties for a market scoped to a company.
func listMarketProperties(ctx context.Context, db gowild_data.Database, companyID, conditionID string) ([]*MarketProperty, error) {
	companyID = strings.TrimSpace(companyID)
	conditionID = strings.TrimSpace(conditionID)
	if companyID == "" || conditionID == "" {
		return nil, nil
	}
	results, err := db.Table(MarketProperty{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"company_id": companyID, "condition_id": conditionID},
		OrderBy: "key",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list market properties: %w", err)
	}
	props := make([]*MarketProperty, 0, len(results))
	for _, r := range results {
		if p, ok := r.(*MarketProperty); ok {
			props = append(props, p)
		}
	}
	return props, nil
}

// deleteMarketProperty removes a single property by key. Returns true if a row was deleted.
func deleteMarketProperty(ctx context.Context, db gowild_data.Database, companyID, conditionID, key string) (bool, error) {
	existing, err := getMarketPropertyRow(ctx, db, strings.TrimSpace(companyID), strings.TrimSpace(conditionID), strings.TrimSpace(key))
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	if err := db.Table(MarketProperty{}).Delete(ctx, existing.ID); err != nil {
		return false, fmt.Errorf("failed to delete market property: %w", err)
	}
	return true, nil
}

// ListMarketPropertiesBulk returns properties for multiple markets in one call.
func ListMarketPropertiesBulk(ctx context.Context, db gowild_data.Database, companyID string, conditionIDs []string) (map[string][]*MarketProperty, error) {
	companyID = strings.TrimSpace(companyID)
	normalized := uniqueMarketConditionIDs(conditionIDs)
	if companyID == "" || len(normalized) == 0 {
		return map[string][]*MarketProperty{}, nil
	}
	values := make([]any, 0, len(normalized))
	for _, id := range normalized {
		values = append(values, id)
	}
	results, err := db.Table(MarketProperty{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"company_id": companyID},
		WhereIn: map[string][]any{"condition_id": values},
		OrderBy: "key",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list market properties bulk: %w", err)
	}
	byMarket := make(map[string][]*MarketProperty, len(normalized))
	for _, id := range normalized {
		byMarket[id] = nil
	}
	for _, r := range results {
		p, ok := r.(*MarketProperty)
		if !ok || p == nil {
			continue
		}
		cid := strings.TrimSpace(p.ConditionID)
		byMarket[cid] = append(byMarket[cid], p)
	}
	return byMarket, nil
}

func getMarketPropertyRow(ctx context.Context, db gowild_data.Database, companyID, conditionID, key string) (*MarketProperty, error) {
	if companyID == "" || conditionID == "" || key == "" {
		return nil, nil
	}
	results, err := db.Table(MarketProperty{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID, "condition_id": conditionID, "key": key},
		Limit: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query market property: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	if p, ok := results[0].(*MarketProperty); ok {
		return p, nil
	}
	return nil, nil
}

func validateMarketPropertyValue(value, valueType string) error {
	switch valueType {
	case MarketPropertyTypeString:
		return nil
	case MarketPropertyTypeDatetime:
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return fmt.Errorf("invalid datetime value (expected RFC3339): %w", err)
		}
		return nil
	case MarketPropertyTypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid bool value: %w", err)
		}
		return nil
	case MarketPropertyTypeCurrency:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("invalid currency value: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported value_type %q (must be string, datetime, bool, or currency)", valueType)
	}
}

// parseMarketPropertyDatetime parses a datetime property value.
func parseMarketPropertyDatetime(prop *MarketProperty) (time.Time, error) {
	if prop == nil {
		return time.Time{}, fmt.Errorf("nil property")
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(prop.Value))
}

// parseMarketPropertyBool parses a bool property value.
func parseMarketPropertyBool(prop *MarketProperty) (bool, error) {
	if prop == nil {
		return false, fmt.Errorf("nil property")
	}
	return strconv.ParseBool(strings.TrimSpace(prop.Value))
}

// parseMarketPropertyCurrency parses a currency property value.
func parseMarketPropertyCurrency(prop *MarketProperty) (float64, error) {
	if prop == nil {
		return 0, fmt.Errorf("nil property")
	}
	return strconv.ParseFloat(strings.TrimSpace(prop.Value), 64)
}
