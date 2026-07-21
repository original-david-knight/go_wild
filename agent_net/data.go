package gowild_agent_net

import (
	"github.com/original-david-knight/go_wild/data"
)

func init() {
	gowild_data.RegisterFunc(AddTables)
}

// AddTables registers all agent_net tables with the database.
// Call this during application initialization.
func AddTables(db gowild_data.Database) error {
	if err := db.AddTable(PremiumAgent{}); err != nil {
		return err
	}
	if err := db.AddTable(RevokedKey{}); err != nil {
		return err
	}
	if err := db.AddTable(Post{}); err != nil {
		return err
	}
	if err := db.AddTable(UsedNonce{}); err != nil {
		return err
	}
	if err := db.AddTable(RateLimit{}); err != nil {
		return err
	}
	if err := db.AddTable(AgentProfile{}); err != nil {
		return err
	}
	if err := db.AddTable(DirectMessage{}); err != nil {
		return err
	}
	if err := db.AddTable(A2AJob{}); err != nil {
		return err
	}
	return db.AddTable(A2AJobEvent{})
}
