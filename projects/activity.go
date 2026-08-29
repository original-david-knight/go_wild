package gowild_projects

import (
	"context"
	"sort"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// Activity kinds: what an entry in the feed came from.
const (
	ActivityComment    = "comment"
	ActivityTransition = "transition"
	ActivityPost       = "post"
	ActivityReply      = "reply"
	ActivityChat       = "chat"
	ActivityRun        = "run"
)

// ActivityBodyMax caps an entry's body, in runes; the source holds the rest.
const ActivityBodyMax = 600

// ActivityEntry is one line of a project's (or every project's) feed: who
// did what, where, when — the owner's answer to "what happened while I was
// away". Exactly the id fields the kind needs are set.
type ActivityEntry struct {
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	Actor     string    `json:"actor"`
	ProjectID string    `json:"project_id"`
	ItemID    string    `json:"item_id"`
	PostID    string    `json:"post_id"`
	CommentID string    `json:"comment_id"`
	ChatID    string    `json:"chat_id"`
	RunID     string    `json:"run_id"`
	// Title is the item's or the post's title, so a line reads without a
	// lookup.
	Title string `json:"title"`
	// Action, FromStatus, ToStatus and Verdict carry a transition's facts.
	Action     string `json:"action"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Verdict    string `json:"verdict"`
	// Body is the comment, the chat line, the post's opening, or the run's
	// summary, capped at ActivityBodyMax.
	Body string `json:"body"`
	// Outcome and JobKind are a run's; Outcome is "" while it is still going.
	Outcome string `json:"outcome"`
	JobKind string `json:"job_kind"`
}

// ActivityFilter narrows the feed. ProjectID "" is every project plus the
// general room; Limit 0 means 50.
type ActivityFilter struct {
	ProjectID string
	Limit     int
}

// Activity is the merged feed, newest first.
func (s *Service) Activity(ctx context.Context, f ActivityFilter) ([]*ActivityEntry, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	scoped := func(where map[string]any) gowild_data.QueryOpts {
		if f.ProjectID != "" {
			where["project_id"] = f.ProjectID
		}
		return gowild_data.QueryOpts{Where: where}
	}

	items, err := gowild_dbx.All[Item](ctx, db, scoped(map[string]any{}))
	if err != nil {
		return nil, err
	}
	itemsByID := make(map[string]*Item, len(items))
	for _, it := range items {
		itemsByID[it.ID] = it
	}
	posts, err := gowild_dbx.All[Post](ctx, db, scoped(map[string]any{}))
	if err != nil {
		return nil, err
	}
	postsByID := make(map[string]*Post, len(posts))
	for _, p := range posts {
		postsByID[p.ID] = p
	}

	var comments []*Comment
	if f.ProjectID == "" {
		if comments, err = gowild_dbx.All[Comment](ctx, db, gowild_data.QueryOpts{}); err != nil {
			return nil, err
		}
	} else {
		targets := make([]any, 0, len(items)+len(posts))
		for _, it := range items {
			targets = append(targets, it.ID)
		}
		for _, p := range posts {
			targets = append(targets, p.ID)
		}
		for start := 0; start < len(targets); start += 200 {
			end := min(start+200, len(targets))
			batch, err := gowild_dbx.All[Comment](ctx, db, gowild_data.QueryOpts{WhereIn: map[string][]any{"target_id": targets[start:end]}})
			if err != nil {
				return nil, err
			}
			comments = append(comments, batch...)
		}
	}
	chat, err := gowild_dbx.All[ChatMessage](ctx, db, scoped(map[string]any{}))
	if err != nil {
		return nil, err
	}
	runs, err := gowild_dbx.All[Run](ctx, db, scoped(map[string]any{}))
	if err != nil {
		return nil, err
	}

	out := make([]*ActivityEntry, 0, len(comments)+len(posts)+len(chat)+len(runs))
	for _, c := range comments {
		e := &ActivityEntry{
			At: c.CreatedAt, Actor: c.Author, CommentID: c.ID, Body: clip(c.Body, ActivityBodyMax),
			Action: c.Action, FromStatus: c.FromStatus, ToStatus: c.ToStatus, Verdict: c.Verdict,
		}
		switch c.TargetKind {
		case TargetItem:
			it := itemsByID[c.TargetID]
			if it == nil {
				continue
			}
			e.ItemID, e.ProjectID, e.Title = it.ID, it.ProjectID, it.Title
			e.Kind = ActivityComment
			if c.Kind == CommentKindTransition {
				e.Kind = ActivityTransition
			}
		case TargetPost:
			p := postsByID[c.TargetID]
			if p == nil {
				continue
			}
			e.PostID, e.ProjectID, e.Title, e.Kind = p.ID, p.ProjectID, p.Title, ActivityReply
		default:
			continue
		}
		out = append(out, e)
	}
	for _, p := range posts {
		out = append(out, &ActivityEntry{
			Kind: ActivityPost, At: p.CreatedAt, Actor: p.Author, ProjectID: p.ProjectID,
			PostID: p.ID, Title: p.Title, Body: clip(p.Body, ActivityBodyMax),
		})
	}
	for _, m := range chat {
		out = append(out, &ActivityEntry{
			Kind: ActivityChat, At: m.CreatedAt, Actor: m.Author, ProjectID: m.ProjectID,
			ChatID: m.ID, Body: clip(m.Body, ActivityBodyMax),
		})
	}
	for _, r := range runs {
		e := &ActivityEntry{
			Kind: ActivityRun, At: r.StartedAt, Actor: r.Agent, ProjectID: r.ProjectID,
			RunID: r.ID, ItemID: r.ItemID, JobKind: r.Kind, Outcome: r.Outcome, Body: clip(r.Summary, ActivityBodyMax),
		}
		if !r.FinishedAt.IsZero() {
			e.At = r.FinishedAt
		}
		if it := itemsByID[r.ItemID]; it != nil {
			e.Title = it.Title
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// clip keeps the first max runes of s.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
