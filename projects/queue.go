package gowild_projects

import (
	"context"
	"errors"
	"fmt"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// RecentChatForJob is how much of a room a job prompt carries.
const RecentChatForJob = 30

// Job is what a check-in hands an agent: the kind of work, the item or the
// mention it concerns, and the project context every job carries.
type Job struct {
	Kind    string   `json:"kind"`
	Project *Project `json:"project"`
	// Item and Comments are set for item jobs; nil for respond.
	Item     *Item      `json:"item"`
	Comments []*Comment `json:"comments"`
	// Pinned and Chat are the project's standing context and its room's
	// recent messages. For the general room Project is nil and Chat is the
	// general room.
	Pinned []*Post        `json:"pinned"`
	Chat   []*ChatMessage `json:"chat"`
	// Mention is set for respond jobs.
	Mention *MentionContext `json:"mention"`
}

// MentionContext is a respond job's target: the mention and the message
// that made it, with the thread it sits in.
type MentionContext struct {
	Mention *Mention `json:"mention"`
	// Exactly one of these is set, by the source kind.
	ChatMessage *ChatMessage `json:"chat_message"`
	Comment     *Comment     `json:"comment"`
	Post        *Post        `json:"post"`
	// Thread is the post's replies or the item's feed when the room is a
	// post or an item; nil for chat (the Job's Chat field is the room).
	Thread []*Comment `json:"thread"`
	// Item is set when the room is an item.
	Item *Item `json:"item"`
	// ThreadPost is the post when the room is a post (Post above is set
	// only when the post body itself made the mention).
	ThreadPost *Post `json:"thread_post"`
}

type candidate struct {
	kind    string
	item    *Item
	project *Project
	mention *Mention
}

// CheckIn records the agent as seen and returns its next job, or nil when
// there is nothing. With claim the item is claimed (or the mention's attempt
// counted) before it is returned, atomically under the service's mutex. A
// name with no row is ErrNotFound: a check-in never creates a worker.
func (s *Service) CheckIn(ctx context.Context, agent string, claim bool) (*Job, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.agentByName(ctx, db, agent)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	a.LastSeenAt = now
	if err := db.Table(Agent{}).Update(ctx, a); err != nil {
		return nil, err
	}
	if !a.Enabled {
		return nil, nil
	}
	candidates, err := s.candidates(ctx, db, agent, now)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if claim {
			if c.mention != nil {
				c.mention.Attempts++
				if err := db.Table(Mention{}).Update(ctx, c.mention); err != nil {
					return nil, err
				}
			} else {
				if _, err := s.applyTransition(ctx, db, c.item, c.project, TransitionInput{Actor: agent, Action: ActionClaim}); err != nil {
					// Lost a race or the rules moved under us: skip it and
					// try the next candidate rather than fail the check-in.
					// Anything else — the store failing mid-claim — has to
					// surface, or an outage reads as "no work".
					if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
						continue
					}
					return nil, err
				}
			}
		}
		job, err := s.buildJob(ctx, db, c)
		if err != nil && claim && c.mention == nil {
			// The claim landed but the job could not be assembled; without a
			// release the agent is wedged behind its own live lease until it
			// expires, and every later check-in answers "no work".
			if _, rerr := s.applyTransition(ctx, db, c.item, c.project, TransitionInput{Actor: agent, Action: ActionRelease, Body: "the check-in could not assemble the job: " + err.Error()}); rerr != nil {
				return nil, fmt.Errorf("build job: %w (and the release failed: %v)", err, rerr)
			}
		}
		return job, err
	}
	return nil, nil
}

// NextFor previews the agent's next job without claiming or recording the
// check-in.
func (s *Service) NextFor(ctx context.Context, agent string) (*Job, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, db, agent, s.Now())
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return s.buildJob(ctx, db, candidates[0])
}

// Wait parks until the agent's queue holds a job, ctx ends or timeout
// passes, and reports whether there is one: true means "check in now". It
// claims nothing and records no check-in. A disabled agent is never ready,
// whatever its queue holds; a name with no row is ErrNotFound. Every wake
// re-runs the check, so a wake for another worker's work parks again. A
// timeout of zero or less checks once. ctx ending is (false, ctx.Err()).
func (s *Service) Wait(ctx context.Context, agent string, timeout time.Duration) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		// The generation is read before the check so a wake between the
		// check and the park closes the channel this turn parks on.
		gen := s.generation()
		ready, err := s.ready(ctx, agent)
		if err != nil || ready {
			return ready, err
		}
		select {
		case <-gen:
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		}
	}
}

// ready is Wait's check: the agent exists, is enabled and has a job. It
// takes no mutex; the writes it races are the ones that wake it.
func (s *Service) ready(ctx context.Context, agent string) (bool, error) {
	db, err := s.database()
	if err != nil {
		return false, err
	}
	a, err := s.agentByName(ctx, db, agent)
	if err != nil {
		return false, err
	}
	if !a.Enabled {
		return false, nil
	}
	job, err := s.NextFor(ctx, agent)
	if err != nil {
		return false, err
	}
	return job != nil, nil
}

// candidates is the queue in order. Only active projects count for item
// work, and only ones with a repository path; mentions count in any active
// project and in the general room.
func (s *Service) candidates(ctx context.Context, db gowild_data.Database, agent string, now time.Time) ([]candidate, error) {
	projects, err := gowild_dbx.All[Project](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Project, len(projects))
	for _, p := range projects {
		byID[p.ID] = p
	}
	workable := func(projectID string) *Project {
		p := byID[projectID]
		if p == nil || p.Status != ProjectActive || p.RepoPath == "" {
			return nil
		}
		return p
	}
	held, err := s.liveLeaseForAgent(ctx, db, agent, now)
	if err != nil {
		return nil, err
	}
	if held != nil {
		return nil, nil
	}
	var out []candidate

	// 0. Mentions.
	mentions, err := s.unansweredMentions(ctx, db, agent)
	if err != nil {
		return nil, err
	}
	for _, m := range mentions {
		var p *Project
		if m.ProjectID != "" {
			p = byID[m.ProjectID]
			if p == nil || p.Status != ProjectActive {
				continue
			}
		}
		out = append(out, candidate{kind: JobRespond, mention: m, project: p})
	}

	// 1. Approved work of mine to finish.
	approved, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"status": StatusApproved, "implementer": agent},
	})
	if err != nil {
		return nil, err
	}
	SortItems(approved)
	for _, it := range approved {
		p := workable(it.ProjectID)
		if p == nil || leaseLive(it, now) || it.Held {
			continue
		}
		kind := JobMerge
		if p.MergePolicy == MergePolicyPullRequest {
			kind = JobPullRequest
		}
		out = append(out, candidate{kind: kind, item: it, project: p})
	}

	// 2. Reviews of the other agent's work.
	inReview, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"status": StatusInReview}})
	if err != nil {
		return nil, err
	}
	SortItems(inReview)
	for _, it := range inReview {
		p := workable(it.ProjectID)
		if p == nil || it.Implementer == agent || it.Held {
			continue
		}
		if it.Reviewer != "" && !LeaseExpired(it, now) {
			continue
		}
		out = append(out, candidate{kind: JobReview, item: it, project: p})
	}

	// 3. In-progress work of mine whose lease is not live: changes
	// requested, or my own runner died. Another worker's item is never
	// offered, whatever the state of its lease.
	inProgress, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"status": StatusInProgress, "assignee": agent},
	})
	if err != nil {
		return nil, err
	}
	SortItems(inProgress)
	for _, it := range inProgress {
		if p := workable(it.ProjectID); p != nil && !leaseLive(it, now) && !it.Held {
			// A resumed item that was never groomed (its groom job crashed)
			// resumes as a groom job, not an implement.
			kind := JobImplement
			if it.NeedsGroom {
				kind = JobGroom
			}
			out = append(out, candidate{kind: kind, item: it, project: p})
		}
	}

	// 4. Open work assigned to me.
	open, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"status": StatusOpen, "assignee": agent},
	})
	if err != nil {
		return nil, err
	}
	SortItems(open)
	for _, it := range open {
		p := workable(it.ProjectID)
		if p == nil || it.Held {
			continue
		}
		// An item waiting on another is not offered until that one is done
		// or closed; the transition that settles it wakes the parked waits.
		settled, err := s.afterSettled(ctx, db, it)
		if err != nil {
			return nil, err
		}
		if !settled {
			continue
		}
		// A raw ticket is groomed into a spec before it is implemented.
		kind := JobImplement
		if it.NeedsGroom {
			kind = JobGroom
		}
		out = append(out, candidate{kind: kind, item: it, project: p})
	}
	return out, nil
}

func leaseLive(it *Item, now time.Time) bool {
	return !it.LeaseExpiresAt.IsZero() && !LeaseExpired(it, now)
}

func holdsLiveLease(it *Item, agent string, now time.Time) bool {
	if !leaseLive(it, now) {
		return false
	}
	switch it.Status {
	case StatusInProgress:
		return it.Assignee == agent
	case StatusInReview:
		return it.Reviewer == agent
	case StatusApproved:
		return it.Implementer == agent
	default:
		return false
	}
}

func (s *Service) liveLeaseForAgent(ctx context.Context, db gowild_data.Database, agent string, now time.Time) (*Item, error) {
	leased, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		WhereIn: map[string][]any{"status": {StatusInProgress, StatusInReview, StatusApproved}},
	})
	if err != nil {
		return nil, err
	}
	for _, it := range leased {
		if holdsLiveLease(it, agent, now) {
			return it, nil
		}
	}
	return nil, nil
}

func (s *Service) buildJob(ctx context.Context, db gowild_data.Database, c candidate) (*Job, error) {
	job := &Job{Kind: c.kind, Project: c.project, Pinned: []*Post{}, Chat: []*ChatMessage{}, Comments: []*Comment{}}
	roomID := ""
	if c.project != nil {
		roomID = c.project.ID
		pinned, err := s.pinnedPosts(ctx, db, c.project.ID)
		if err != nil {
			return nil, err
		}
		job.Pinned = pinned
	}
	chat, err := s.recentChat(ctx, db, roomID, RecentChatForJob)
	if err != nil {
		return nil, err
	}
	job.Chat = chat
	if c.item != nil {
		job.Item = c.item
		comments, err := s.comments(ctx, db, TargetItem, c.item.ID)
		if err != nil {
			return nil, err
		}
		job.Comments = comments
		return job, nil
	}
	if c.mention != nil {
		mc, err := s.mentionContext(ctx, db, c.mention)
		if err != nil {
			return nil, err
		}
		job.Mention = mc
	}
	return job, nil
}

func (s *Service) mentionContext(ctx context.Context, db gowild_data.Database, m *Mention) (*MentionContext, error) {
	mc := &MentionContext{Mention: m, Thread: []*Comment{}}
	switch m.SourceKind {
	case "chat":
		msg, err := gowild_dbx.Get[ChatMessage](ctx, db, m.SourceID)
		if err != nil {
			return nil, err
		}
		mc.ChatMessage = msg
	case "post":
		post, err := gowild_dbx.Get[Post](ctx, db, m.SourceID)
		if err != nil {
			return nil, err
		}
		mc.Post = post
	case "comment":
		c, err := gowild_dbx.Get[Comment](ctx, db, m.SourceID)
		if err != nil {
			return nil, err
		}
		mc.Comment = c
	case "item":
		it, err := gowild_dbx.Get[Item](ctx, db, m.SourceID)
		if err != nil {
			return nil, err
		}
		mc.Item = it
	default:
		return nil, fmt.Errorf("mention %s has unknown source %q", m.ID, m.SourceKind)
	}
	switch m.Room {
	case RoomPost:
		post, err := gowild_dbx.Get[Post](ctx, db, m.RoomID)
		if err != nil {
			return nil, err
		}
		mc.ThreadPost = post
		thread, err := s.comments(ctx, db, TargetPost, m.RoomID)
		if err != nil {
			return nil, err
		}
		mc.Thread = thread
	case RoomItem:
		it, err := gowild_dbx.Get[Item](ctx, db, m.RoomID)
		if err != nil {
			return nil, err
		}
		mc.Item = it
		thread, err := s.comments(ctx, db, TargetItem, m.RoomID)
		if err != nil {
			return nil, err
		}
		mc.Thread = thread
	}
	return mc, nil
}
