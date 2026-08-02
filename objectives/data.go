package objectives

import (
	"github.com/original-david-knight/go_wild/data"
)

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		if err := db.AddTable(Objective{}); err != nil {
			return err
		}
		if err := db.AddTable(ActivityEvent{}); err != nil {
			return err
		}
		return db.AddTable(Escalation{})
	})
}
