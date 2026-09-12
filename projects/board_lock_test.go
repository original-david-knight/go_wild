package gowild_projects

import (
	"errors"
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestPostGuardAndNestedConversationRollback(t *testing.T) {
	f := newFixture(t)
	f.project("BOARD")
	p, err := f.s.CreatePost(f.ctx, "BOARD", PostInput{Author: ActorOwner, Title: "Design", Body: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.GuardPost(f.ctx, p.ID); err == nil {
		t.Fatal("post guard outside transaction accepted")
	}
	abort := errors.New("fixture rollback")
	err = f.s.InTransaction(f.ctx, func(service *Service, db gowild_data.Database) error {
		if _, _, err := service.GuardPost(f.ctx, p.ID); err != nil {
			return err
		}
		body := "Edited"
		if _, err := service.UpdatePost(f.ctx, p.ID, ActorOwner, PostPatch{Body: &body}); err != nil {
			return err
		}
		if _, err := service.ReplyToPost(f.ctx, p.ID, ActorOwner, "Reply"); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	after, _, err := f.s.GetPost(f.ctx, p.ID)
	if err != nil || after.Body != "Original" || after.ReplyCount != 0 {
		t.Fatalf("partial conversation commit: %+v %v", after, err)
	}
	replies, err := f.s.PostReplies(f.ctx, p.ID)
	if err != nil || len(replies) != 0 {
		t.Fatalf("partial reply commit: %+v %v", replies, err)
	}
}
