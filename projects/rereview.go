package gowild_projects

import (
	"context"

	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// QueueReview reuses an earlier PR ticket, including a retained deletion or
// a legacy personal review task. An already queued or running review is a no-op.
// The caller resolves the PR identity; this method owns the atomic handoff.
// An optional revision rejects a stale connector observation.
func (s *Service) QueueReview(ctx context.Context, id, prURL string, revision ...int) (*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := gowild_dbx.Get[Item](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, ErrNotFound
	}
	if len(revision) > 0 && it.Revision != revision[0] {
		return nil, ErrConflict
	}
	if it.Type != TypeCodeReview && it.Type != TypeTask {
		return nil, invalidf("the earlier ticket is already being used for other work")
	}
	if it.Type == TypeCodeReview && !it.Deleted && !it.Held &&
		(it.Status == StatusOpen || it.Status == StatusInProgress && leaseLive(it, s.Now())) {
		return it, nil
	}
	p, err := gowild_dbx.Get[Project](ctx, db, it.ProjectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, validationf("reviewing needs a project with a repository")
	}
	url, err := cleanPRURL(prURL)
	if err != nil {
		return nil, err
	}
	if url == "" {
		return nil, validationf("a code_review item needs pr_url")
	}
	it.Type, it.PRURL, it.Deleted = TypeCodeReview, url, false
	if it.Number == 0 {
		it.Number = p.NextNumber
		if it.Number < 1 {
			it.Number = 1
		}
		p.NextNumber = it.Number + 1
		if err := db.Table(Project{}).Update(ctx, p); err != nil {
			return nil, err
		}
	}
	if _, err := s.applyTransition(ctx, db, it, p, TransitionInput{Actor: ActorOwner, Action: ActionRereview}); err != nil {
		return nil, err
	}
	s.wake()
	return it, nil
}

// CompleteReview records a review submitted by the owner on the PR host.
// The connector supplies the observed revision so a concurrent manual request
// cannot be cleared by an older observation. A running worker finishes its
// handoff first; the connector retries on its next poll.
func (s *Service) CompleteReview(ctx context.Context, id string, revision int, body string) (*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, p, err := s.itemByKey(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if it.Revision != revision || leaseLive(it, s.Now()) {
		return nil, ErrConflict
	}
	if it.Type != TypeCodeReview {
		return nil, invalidf("only a code review can complete from a submitted PR review")
	}
	if Ended(it.Status) {
		return it, nil
	}
	_, err = s.applyTransition(ctx, db, it, p, TransitionInput{
		Actor: ActorOwner, Action: ActionApprove, Body: body, reviewSubmitted: true,
	})
	if err != nil {
		return nil, err
	}
	s.wake()
	return it, nil
}
