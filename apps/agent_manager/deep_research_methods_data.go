package main

import (
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// DeepResearchMethod stores a configurable deep-research method definition.
// Method is the primary key and doubles as the future tool/method name.
type DeepResearchMethod struct {
	Method             string    `json:"method" db:"id"`
	Description        string    `json:"description"`
	Instructions       string    `json:"instructions"`
	QueryTemplate      string    `json:"query_template"`
	InputSchemaJSON    string    `json:"input_schema_json"`
	ResearchSchemaJSON string    `json:"research_schema_json"`
	OptionsJSON        string    `json:"options_json"`
	Enabled            bool      `json:"enabled"`
	LastTestedAt       time.Time `json:"last_tested_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (DeepResearchMethod) TableName() string { return "deep_research_methods" }

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		return db.AddTable(DeepResearchMethod{})
	})
}
