package gowild_projects

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// ItemInput is what filing an item takes.
type ItemInput struct {
	Type        string
	Title       string
	Description string
	Priority    string
	CreatedBy   string
	// Assignee hands the item to one worker from the start; "" stamps the
	// default worker.
	Assignee string
	// Label tags the item for grouping; "" is no label.
	Label string
	// After names an item in the same project this one waits on; "" is no
	// dependency.
	After string
	// Held files the item parked: the queue skips it until the owner
	// lifts the hold.
	Held bool
}

// ItemPatch is a partial edit of the descriptive fields; nil leaves a field
// alone.
type ItemPatch struct {
	Title       *string
	Description *string
	Type        *string
	Priority    *string
	// Assignee reassigns an item to another worker, never to nobody. It is
	// accepted while the item is open, or in progress with an expired lease
	// (the holder's runner died); any other status refuses it — live work is
	// not the owner's to swap.
	Assignee *string
	// Label retags the item; the empty string clears it.
	Label *string
	// After re-points or clears the dependency; the empty string clears it.
	After *string
}

// ItemFilter narrows a listing. With no Statuses, done and closed items are
// left out unless IncludeClosed asks for them.
type ItemFilter struct {
	ProjectKey    string
	Statuses      []string
	Assignee      string
	Label         string
	IncludeClosed bool
}

// TransitionInput drives the state machine. Actor is ActorOwner or an agent
// name; the other fields matter to the actions that read them.
type TransitionInput struct {
	Actor   string
	Action  string
	Body    string
	Branch  string
	PRURL   string
	Verdict string
}

// CreateItem files an item under the project, numbering it from the
// project's counter.
func (s *Service) CreateItem(ctx context.Context, projectKey string, in ItemInput) (*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	if in.Type == "" {
		in.Type = TypeFeature
	}
	if !validType(in.Type) {
		return nil, validationf("item type %q is not feature, bug or chore", in.Type)
	}
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !validPriority(in.Priority) {
		return nil, validationf("priority %q is not low, normal, high or urgent", in.Priority)
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, validationf("item title is required")
	}
	if in.CreatedBy == "" {
		in.CreatedBy = ActorOwner
	}
	in.Assignee = strings.TrimSpace(in.Assignee)
	if in.Assignee != "" && !ValidAgentName(in.Assignee) {
		return nil, validationf("assignee %q is not an agent name", in.Assignee)
	}
	label, err := cleanLabel(in.Label)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.projectByKey(ctx, db, projectKey)
	if err != nil {
		return nil, err
	}
	// The dependency is resolved under the mutex so the item it names
	// cannot vanish between the check and the write. Nothing references a
	// new item yet, so creation cannot make a cycle.
	after, err := s.resolveAfter(ctx, db, p, "", in.After)
	if err != nil {
		return nil, err
	}
	// Every item has a worker: the one named, else the default. Only a
	// named assignee is a hand-off in the feed; the default is stamped.
	named := in.Assignee != ""
	if named {
		if _, err := s.agentByName(ctx, db, in.Assignee); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, validationf("assignee %q is not a worker", in.Assignee)
			}
			return nil, err
		}
	} else {
		def, err := s.defaultAgent(ctx, db)
		if err != nil {
			return nil, err
		}
		if def == nil {
			return nil, validationf("there is no worker to assign the item to; create an agent first")
		}
		in.Assignee = def.ID
	}
	now := s.Now()
	number := p.NextNumber
	if number < 1 {
		number = 1
	}
	p.NextNumber = number + 1
	p.UpdatedAt = now
	if err := db.Table(Project{}).Update(ctx, p); err != nil {
		return nil, err
	}
	it := &Item{
		ID: newID(), ProjectID: p.ID, Number: number, Type: in.Type,
		Title: strings.TrimSpace(in.Title), Description: in.Description, Priority: in.Priority,
		Status: StatusOpen, Assignee: in.Assignee, Label: label, After: after, Held: in.Held,
		CreatedBy: in.CreatedBy, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Table(Item{}).Insert(ctx, it); err != nil {
		return nil, err
	}
	if named {
		if err := s.recordAssign(ctx, db, it, in.CreatedBy, in.Assignee, now); err != nil {
			return nil, err
		}
	}
	if err := s.recordMentions(ctx, db, it.Description, RoomItem, it.ID, p.ID, "item", it.ID, in.CreatedBy, now); err != nil {
		return nil, err
	}
	s.wake()
	return it, nil
}

// GetItem reads an item and its project by key ("EA-12").
func (s *Service) GetItem(ctx context.Context, key string) (*Item, *Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, nil, err
	}
	return s.itemByKey(ctx, db, key)
}

func (s *Service) itemByKey(ctx context.Context, db gowild_data.Database, key string) (*Item, *Project, error) {
	projectKey, number, err := ParseItemKey(key)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.projectByKey(ctx, db, projectKey)
	if err != nil {
		return nil, nil, err
	}
	rows, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"project_id": p.ID, "number": number}, Limit: 1,
	})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("%w: item %s-%d", ErrNotFound, p.Key, number)
	}
	return rows[0], p, nil
}

func (s *Service) itemByID(ctx context.Context, db gowild_data.Database, id string) (*Item, error) {
	it, err := gowild_dbx.Get[Item](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, fmt.Errorf("%w: item id %s", ErrNotFound, id)
	}
	return it, nil
}

// ListItems returns items matching the filter, attention-first: status
// group, then priority (urgent first), then age (oldest first).
func (s *Service) ListItems(ctx context.Context, f ItemFilter) ([]*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	opts := gowild_data.QueryOpts{Where: map[string]any{}}
	if f.ProjectKey != "" {
		p, err := s.projectByKey(ctx, db, f.ProjectKey)
		if err != nil {
			return nil, err
		}
		opts.Where["project_id"] = p.ID
	}
	if f.Assignee != "" {
		opts.Where["assignee"] = f.Assignee
	}
	if f.Label != "" {
		opts.Where["label"] = f.Label
	}
	if len(f.Statuses) > 0 {
		vals := make([]any, 0, len(f.Statuses))
		for _, st := range f.Statuses {
			if !validStatus(st) {
				return nil, validationf("status %q is unknown", st)
			}
			vals = append(vals, st)
		}
		opts.WhereIn = map[string][]any{"status": vals}
	}
	rows, err := gowild_dbx.All[Item](ctx, db, opts)
	if err != nil {
		return nil, err
	}
	if len(f.Statuses) == 0 && !f.IncludeClosed {
		kept := rows[:0]
		for _, it := range rows {
			if it.Status != StatusDone && it.Status != StatusClosed {
				kept = append(kept, it)
			}
		}
		rows = kept
	}
	SortItems(rows)
	return rows, nil
}

// SortItems orders items attention-first, in place.
func SortItems(rows []*Item) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if StatusOrder(a.Status) != StatusOrder(b.Status) {
			return StatusOrder(a.Status) < StatusOrder(b.Status)
		}
		if PriorityRank(a.Priority) != PriorityRank(b.Priority) {
			return PriorityRank(a.Priority) > PriorityRank(b.Priority)
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}

// UpdateItem edits the descriptive fields. baseRevision > 0 is a
// precondition: the stored revision must match or ErrStaleRevision.
func (s *Service) UpdateItem(ctx context.Context, key string, patch ItemPatch, baseRevision int) (*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, p, err := s.itemByKey(ctx, db, key)
	if err != nil {
		return nil, err
	}
	if baseRevision > 0 && it.Revision != baseRevision {
		return nil, fmt.Errorf("%w: item is at revision %d, not %d", ErrStaleRevision, it.Revision, baseRevision)
	}
	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return nil, validationf("item title is required")
		}
		it.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Description != nil {
		it.Description = *patch.Description
	}
	if patch.Type != nil {
		if !validType(*patch.Type) {
			return nil, validationf("item type %q is not feature, bug or chore", *patch.Type)
		}
		it.Type = *patch.Type
	}
	if patch.Priority != nil {
		if !validPriority(*patch.Priority) {
			return nil, validationf("priority %q is not low, normal, high or urgent", *patch.Priority)
		}
		it.Priority = *patch.Priority
	}
	if patch.Label != nil {
		label, err := cleanLabel(*patch.Label)
		if err != nil {
			return nil, err
		}
		it.Label = label
	}
	afterChanged := false
	if patch.After != nil {
		after, err := s.resolveAfter(ctx, db, p, it.ID, *patch.After)
		if err != nil {
			return nil, err
		}
		if after != "" {
			// Re-pointing must not close a loop through the chain above.
			loops, err := s.afterChainLoops(ctx, db, after, ItemKey(p, it))
			if err != nil {
				return nil, err
			}
			if loops {
				return nil, validationf("after %s would loop back to this item", after)
			}
		}
		afterChanged = it.After != after
		it.After = after
	}
	now := s.Now()
	assignTo, assignFrom, assignChanged := "", "", false
	if patch.Assignee != nil {
		assignTo = strings.TrimSpace(*patch.Assignee)
		if assignTo == "" {
			return nil, validationf("assignee is required; every item has a worker")
		}
		if !ValidAgentName(assignTo) {
			return nil, validationf("assignee %q is not an agent name", assignTo)
		}
		if assignTo != it.Assignee {
			switch {
			case it.Status == StatusOpen:
			case it.Status == StatusInProgress && LeaseExpired(it, now):
				// The holder's runner died. The item keeps its branch and
				// implementer and resumes under the new worker from its
				// next check-in; the dead holder's lease goes.
				assignFrom = it.Assignee
				it.ClaimedAt = time.Time{}
				it.LeaseExpiresAt = time.Time{}
			case it.Status == StatusInProgress:
				return nil, invalidf("%s holds this item and its lease has not expired", it.Assignee)
			default:
				return nil, invalidf("only an open item, or an in-progress one whose lease expired, is reassigned; this one is %s", it.Status)
			}
			if _, err := s.agentByName(ctx, db, assignTo); err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil, validationf("assignee %q is not a worker", assignTo)
				}
				return nil, err
			}
			it.Assignee = assignTo
			assignChanged = true
		}
	}
	it.Revision++
	it.UpdatedAt = now
	if err := db.Table(Item{}).Update(ctx, it); err != nil {
		return nil, err
	}
	if assignChanged {
		if err := s.recordAssign(ctx, db, it, ActorOwner, assignTo, now); err != nil {
			return nil, err
		}
		if assignFrom != "" {
			if err := s.setAgentCurrent(ctx, db, assignFrom, ""); err != nil {
				return nil, err
			}
		}
	}
	if patch.Description != nil {
		if err := s.syncMentions(ctx, db, it.Description, RoomItem, it.ID, p.ID, "item", it.ID, ActorOwner, it.UpdatedAt); err != nil {
			return nil, err
		}
	}
	// A new assignee has open work; a new description may name someone; a
	// cleared or re-pointed dependency may free an item. The other fields
	// move no work.
	if assignChanged || afterChanged || patch.Description != nil {
		s.wake()
	}
	return it, nil
}

// cleanLabel trims a label and refuses one that cannot sit in a table row:
// more than 64 runes, or a line break.
func cleanLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if len([]rune(label)) > 64 {
		return "", validationf("label is longer than 64 characters")
	}
	if strings.ContainsAny(label, "\r\n") {
		return "", validationf("label must be one line")
	}
	return label, nil
}

// resolveAfter canonicalises a dependency key: "" is none; otherwise it must
// name an existing item in the same project and not the item itself.
func (s *Service) resolveAfter(ctx context.Context, db gowild_data.Database, p *Project, selfID, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	target, tp, err := s.itemByKey(ctx, db, raw)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", validationf("after %q names no item", strings.TrimSpace(raw))
		}
		return "", err
	}
	if tp.ID != p.ID {
		return "", validationf("after must name an item in this project; %s is in %s", ItemKey(tp, target), tp.Key)
	}
	if selfID != "" && target.ID == selfID {
		return "", validationf("an item cannot wait on itself")
	}
	return ItemKey(tp, target), nil
}

// afterChainLoops walks the dependency chain from startKey and reports
// whether it reaches selfKey. Chains are short; the cap only guards a
// corrupt store.
func (s *Service) afterChainLoops(ctx context.Context, db gowild_data.Database, startKey, selfKey string) (bool, error) {
	key := startKey
	for hops := 0; key != "" && hops < 100; hops++ {
		if key == selfKey {
			return true, nil
		}
		it, _, err := s.itemByKey(ctx, db, key)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		key = it.After
	}
	return false, nil
}

// afterSettled reports whether the item's dependency is settled: no after,
// or the item it names is done or closed either way. An after whose item is
// gone blocks nothing.
func (s *Service) afterSettled(ctx context.Context, db gowild_data.Database, it *Item) (bool, error) {
	if it.After == "" {
		return true, nil
	}
	target, _, err := s.itemByKey(ctx, db, it.After)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation) {
			return true, nil
		}
		return false, err
	}
	return target.Status == StatusDone || target.Status == StatusClosed, nil
}

// recordAssign puts the hand-off in the feed as a transition that moves no
// status: "assigned to fable".
func (s *Service) recordAssign(ctx context.Context, db gowild_data.Database, it *Item, actor, assignee string, now time.Time) error {
	c := &Comment{
		ID: newID(), TargetKind: TargetItem, TargetID: it.ID, Author: actor,
		Kind: CommentKindTransition, Action: ActionAssign, FromStatus: it.Status, ToStatus: it.Status,
		Body: "assigned to " + assignee, CreatedAt: now,
	}
	return db.Table(Comment{}).Insert(ctx, c)
}

// LeaseExpired reports whether the item's holder is past its lease at now.
// An item with no lease is never expired.
func LeaseExpired(it *Item, now time.Time) bool {
	return !it.LeaseExpiresAt.IsZero() && now.After(it.LeaseExpiresAt)
}

// Transition applies one action to the item and records it in the feed.
func (s *Service) Transition(ctx context.Context, key string, in TransitionInput) (*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, p, err := s.itemByKey(ctx, db, key)
	if err != nil {
		return nil, err
	}
	if _, err := s.applyTransition(ctx, db, it, p, in); err != nil {
		return nil, err
	}
	// Every action can put work in front of someone: submit a review,
	// approve a merge, request_changes and reopen an implement, release
	// and block a free worker.
	s.wake()
	return it, nil
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidTransition, fmt.Sprintf(format, args...))
}

func forbiddenf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrForbidden, fmt.Sprintf(format, args...))
}

// applyTransition is the state machine. It mutates it in place, writes the
// item, the transition comment and the agent bookkeeping, and returns the
// comment. The caller holds the mutex.
func (s *Service) applyTransition(ctx context.Context, db gowild_data.Database, it *Item, p *Project, in TransitionInput) (*Comment, error) {
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		return nil, validationf("actor is required")
	}
	isOwner := actor == ActorOwner
	if !isOwner && !ValidAgentName(actor) {
		return nil, validationf("actor %q is not owner or an agent name", actor)
	}
	now := s.Now()
	from := it.Status
	expired := LeaseExpired(it, now)
	body := strings.TrimSpace(in.Body)
	verdict := ""
	// agentHolds / agentClears drive the agents table after the write.
	holdBy, clearFor := "", ""
	var to string

	lease := func() {
		it.ClaimedAt = now
		it.LeaseExpiresAt = now.Add(s.lease)
	}
	clearLease := func() {
		it.ClaimedAt = time.Time{}
		it.LeaseExpiresAt = time.Time{}
	}

	switch in.Action {
	case ActionClaim:
		if isOwner {
			return nil, forbiddenf("the owner does not claim work")
		}
		// Only a worker the owner created claims; a claim never creates
		// the row.
		if _, err := s.agentByName(ctx, db, actor); err != nil {
			return nil, err
		}
		held, err := s.liveLeaseForAgent(ctx, db, actor, now)
		if err != nil {
			return nil, err
		}
		if held != nil && held.ID != it.ID {
			return nil, invalidf("%s already holds live work", actor)
		}
		switch from {
		case StatusOpen:
			if it.Assignee != "" && it.Assignee != actor {
				return nil, invalidf("%s is assigned to %s", from, it.Assignee)
			}
			if it.Held {
				return nil, invalidf("held by the owner")
			}
			settled, err := s.afterSettled(ctx, db, it)
			if err != nil {
				return nil, err
			}
			if !settled {
				return nil, invalidf("waits on %s", it.After)
			}
			to = StatusInProgress
			it.Assignee = actor
			lease()
			holdBy = actor
		case StatusInProgress:
			if leaseLive(it, now) {
				return nil, invalidf("held by %s until %s", it.Assignee, it.LeaseExpiresAt.Format(time.RFC3339))
			}
			// An expired lease is a dead runner; the work stays with its
			// worker until the owner reassigns it.
			if it.Assignee != actor {
				return nil, forbiddenf("assigned to %s", it.Assignee)
			}
			to = StatusInProgress
			lease()
			holdBy = actor
		case StatusInReview:
			if actor == it.Implementer {
				return nil, forbiddenf("%s implemented this and cannot review it", actor)
			}
			if leaseLive(it, now) {
				return nil, invalidf("under review by %s", it.Reviewer)
			}
			to = StatusInReview
			it.Reviewer = actor
			lease()
			holdBy = actor
		case StatusApproved:
			if actor != it.Implementer {
				return nil, forbiddenf("only the implementer %s finishes an approved item", it.Implementer)
			}
			if leaseLive(it, now) {
				return nil, invalidf("held by %s until %s", actor, it.LeaseExpiresAt.Format(time.RFC3339))
			}
			to = StatusApproved
			lease()
			holdBy = actor
		default:
			return nil, invalidf("cannot claim from %s", from)
		}
	case ActionRelease:
		if isOwner {
			return nil, forbiddenf("the owner does not release work; reopen or close it")
		}
		switch from {
		case StatusInProgress:
			if it.Assignee != actor {
				return nil, forbiddenf("held by %s", it.Assignee)
			}
			// The worker keeps the item: its next check-in retries it.
			to = StatusOpen
			clearLease()
			clearFor = actor
		case StatusInReview:
			if it.Reviewer != actor {
				return nil, forbiddenf("reviewed by %s", it.Reviewer)
			}
			to = StatusInReview
			it.Reviewer = ""
			clearLease()
			clearFor = actor
		case StatusApproved:
			if it.Implementer != actor {
				return nil, forbiddenf("belongs to %s", it.Implementer)
			}
			to = StatusApproved
			clearLease()
			clearFor = actor
		default:
			return nil, invalidf("cannot release from %s", from)
		}
	case ActionSubmit:
		if isOwner {
			return nil, forbiddenf("the owner does not submit work")
		}
		if from != StatusInProgress {
			return nil, invalidf("cannot submit from %s", from)
		}
		if it.Assignee != actor {
			return nil, forbiddenf("held by %s", it.Assignee)
		}
		branch := strings.TrimSpace(in.Branch)
		if branch == "" {
			branch = it.Branch
		}
		if branch == "" {
			return nil, validationf("submit needs a branch")
		}
		to = StatusInReview
		it.Branch = branch
		if strings.TrimSpace(in.PRURL) != "" {
			it.PRURL = strings.TrimSpace(in.PRURL)
		}
		it.Implementer = actor
		it.Assignee = ""
		it.Reviewer = ""
		clearLease()
		clearFor = actor
	case ActionReview:
		if isOwner {
			return nil, forbiddenf("the owner approves or requests changes, not reviews")
		}
		if from != StatusInReview {
			return nil, invalidf("cannot review from %s", from)
		}
		if actor == it.Implementer {
			return nil, forbiddenf("%s implemented this and cannot review it", actor)
		}
		if it.Reviewer != "" && it.Reviewer != actor && !expired {
			return nil, invalidf("under review by %s", it.Reviewer)
		}
		verdict = in.Verdict
		switch verdict {
		case VerdictApprove:
			to = StatusPendingApproval
			it.Assignee = ""
		case VerdictRequestChanges:
			if body == "" {
				return nil, validationf("request_changes needs a review body")
			}
			to = StatusInProgress
			it.Assignee = it.Implementer
		default:
			return nil, validationf("verdict %q is not approve or request_changes", in.Verdict)
		}
		it.Reviewer = actor
		it.LastVerdict, it.LastVerdictBy, it.LastVerdictAt = verdict, actor, now
		clearLease()
		clearFor = actor
	case ActionBlock:
		if isOwner {
			return nil, forbiddenf("the owner closes or reopens, not blocks")
		}
		if from != StatusInProgress {
			return nil, invalidf("cannot block from %s", from)
		}
		if it.Assignee != actor {
			return nil, forbiddenf("held by %s", it.Assignee)
		}
		if body == "" {
			return nil, validationf("block needs the question for the owner")
		}
		// Blocked, not held: the assignee stays so reopen returns the item
		// to the same worker, but the lease and the agent's current item go.
		to = StatusBlocked
		clearLease()
		clearFor = actor
	case ActionApprove:
		if !isOwner {
			return nil, forbiddenf("only the owner approves")
		}
		if from != StatusPendingApproval {
			return nil, invalidf("cannot approve from %s", from)
		}
		to = StatusApproved
		it.Assignee = ""
		clearLease()
	case ActionRequestChanges:
		if !isOwner {
			return nil, forbiddenf("only the owner requests changes")
		}
		if from != StatusPendingApproval {
			return nil, invalidf("cannot request changes from %s", from)
		}
		if body == "" {
			return nil, validationf("request_changes needs a body")
		}
		to = StatusInProgress
		it.Assignee = it.Implementer
		it.LastVerdict, it.LastVerdictBy, it.LastVerdictAt = VerdictRequestChanges, actor, now
		clearLease()
	case ActionComplete:
		if !isOwner && actor != it.Implementer {
			return nil, forbiddenf("only the implementer %s or the owner completes", it.Implementer)
		}
		if from != StatusApproved {
			return nil, invalidf("cannot complete from %s", from)
		}
		if strings.TrimSpace(in.PRURL) != "" {
			it.PRURL = strings.TrimSpace(in.PRURL)
		}
		to = StatusDone
		it.Assignee = ""
		it.ClosedAt = now
		clearLease()
		if !isOwner {
			clearFor = actor
		} else if it.Implementer != "" {
			clearFor = it.Implementer
		}
	case ActionReopen:
		if !isOwner {
			return nil, forbiddenf("only the owner reopens")
		}
		if from != StatusBlocked && from != StatusDone && from != StatusClosed {
			return nil, invalidf("cannot reopen from %s", from)
		}
		to = StatusOpen
		if it.Assignee == "" {
			// A done or closed item lost its holder on the way out: it goes
			// back to its implementer, else to the default worker.
			it.Assignee = it.Implementer
			if it.Assignee == "" {
				def, err := s.defaultAgent(ctx, db)
				if err != nil {
					return nil, err
				}
				if def == nil {
					return nil, validationf("there is no worker to reopen the item for; create an agent first")
				}
				it.Assignee = def.ID
			}
		}
		it.Reviewer = ""
		it.ClosedAt = time.Time{}
		clearLease()
	case ActionHold:
		if !isOwner {
			return nil, forbiddenf("only the owner holds an item")
		}
		if from != StatusOpen {
			return nil, invalidf("cannot hold from %s", from)
		}
		if it.Held {
			return nil, invalidf("already held")
		}
		it.Held = true
		to = StatusOpen
	case ActionUnhold:
		if !isOwner {
			return nil, forbiddenf("only the owner lifts a hold")
		}
		// A held item that was closed keeps its hold; reopen brings it back
		// still parked, and the unhold happens there.
		if from != StatusOpen {
			return nil, invalidf("cannot unhold from %s", from)
		}
		if !it.Held {
			return nil, invalidf("not held")
		}
		it.Held = false
		to = StatusOpen
	case ActionClose:
		if !isOwner {
			return nil, forbiddenf("only the owner closes")
		}
		if from == StatusDone || from == StatusClosed {
			return nil, invalidf("cannot close from %s", from)
		}
		if it.Assignee != "" {
			clearFor = it.Assignee
		} else if from == StatusInReview && it.Reviewer != "" {
			clearFor = it.Reviewer
		}
		to = StatusClosed
		it.Assignee = ""
		it.ClosedAt = now
		clearLease()
	default:
		return nil, validationf("action %q is unknown", in.Action)
	}

	it.Status = to
	it.Revision++
	it.UpdatedAt = now
	if err := db.Table(Item{}).Update(ctx, it); err != nil {
		return nil, err
	}
	c := &Comment{
		ID: newID(), TargetKind: TargetItem, TargetID: it.ID, Author: actor,
		Kind: CommentKindTransition, Action: in.Action, FromStatus: from, ToStatus: to,
		Verdict: verdict, Body: body, Mentions: joinMentions(ExtractMentions(body)), CreatedAt: now,
	}
	if err := db.Table(Comment{}).Insert(ctx, c); err != nil {
		return nil, err
	}
	if err := s.recordMentions(ctx, db, body, RoomItem, it.ID, p.ID, "comment", c.ID, actor, now); err != nil {
		return nil, err
	}
	if !isOwner {
		if err := s.markAnswered(ctx, db, actor, RoomItem, it.ID, now); err != nil {
			return nil, err
		}
	}
	if clearFor != "" && clearFor != holdBy {
		if err := s.setAgentCurrent(ctx, db, clearFor, ""); err != nil {
			return nil, err
		}
	}
	if holdBy != "" {
		if err := s.setAgentCurrent(ctx, db, holdBy, it.ID); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// ItemComments is the item's feed, oldest first.
func (s *Service) ItemComments(ctx context.Context, itemID string) ([]*Comment, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.comments(ctx, db, TargetItem, itemID)
}

// CommentOnItem adds prose to an item's feed.
func (s *Service) CommentOnItem(ctx context.Context, key, author, body string) (*Comment, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, p, err := s.itemByKey(ctx, db, key)
	if err != nil {
		return nil, err
	}
	c, err := s.addComment(ctx, db, TargetItem, it.ID, p.ID, author, body)
	if err != nil {
		return nil, err
	}
	s.wake()
	return c, nil
}
