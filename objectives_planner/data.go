package objectives_planner

import (
	"github.com/original-david-knight/go_wild/data"
)

// The planner's memory tables register only when the planner is imported, so
// a consumer that just wants objectives does not materialise them.
func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		if err := db.AddTable(KnowledgeEntry{}); err != nil {
			return err
		}
		if err := db.AddTable(DecisionEntry{}); err != nil {
			return err
		}
		return db.AddTable(LearningEntry{})
	})
}
