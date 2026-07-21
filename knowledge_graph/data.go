package gowild_knowledge_graph

import (
	"github.com/original-david-knight/go_wild/data"
)

// init registers the knowledge graph models with the data layer.
// This enables automatic table creation when the database is initialized.
func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		if err := db.AddTable(Node{}); err != nil {
			return err
		}
		return db.AddTable(Edge{})
	})
}
