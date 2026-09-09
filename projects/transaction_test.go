package gowild_projects

import (
	"errors"
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestCompoundAssignmentRollsBackAndWakesOnlyAfterCommit(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	it := f.item("EA", "Implement retry")
	before := f.s.generation()
	refused := errors.New("refused final step")
	assignee := "codex"
	err := f.s.InTransaction(f.ctx, func(s *Service, _ gowild_data.Database) error {
		if _, err := s.UpdateItem(f.ctx, it.ID, ItemPatch{Assignee: &assignee}, it.Revision); err != nil {
			return err
		}
		select {
		case <-before:
			t.Fatal("workers woke before commit")
		default:
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatal(err)
	}
	row, _, err := f.s.GetItem(f.ctx, it.ID)
	if err != nil || row.Assignee != it.Assignee || row.Revision != it.Revision {
		t.Fatalf("partial assignment: %+v %v", row, err)
	}
	comments, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("partial history: %+v %v", comments, err)
	}
	select {
	case <-before:
		t.Fatal("failed transaction woke workers")
	default:
	}
	if err := f.s.InTransaction(f.ctx, func(s *Service, _ gowild_data.Database) error {
		_, err := s.UpdateItem(f.ctx, it.ID, ItemPatch{Assignee: &assignee}, it.Revision)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-before:
	default:
		t.Fatal("committed assignment did not wake workers")
	}
}
