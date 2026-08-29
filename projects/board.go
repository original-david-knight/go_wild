package gowild_projects

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// MaxMentionAttempts caps how often a respond job is handed out for one
// mention before it is left alone.
const MaxMentionAttempts = 3

// ---------------------------------------------------------------- comments

func (s *Service) comments(ctx context.Context, db gowild_data.Database, targetKind, targetID string) ([]*Comment, error) {
	rows, err := gowild_dbx.All[Comment](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"target_kind": targetKind, "target_id": targetID},
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	return rows, nil
}

func validActor(actor string) error {
	if actor == ActorOwner || ValidAgentName(actor) {
		return nil
	}
	return validationf("author %q is not owner or an agent name", actor)
}

// addComment writes prose on an item or post, records its mentions, and
// counts it as the author answering any mention of them there. The caller
// holds the mutex.
func (s *Service) addComment(ctx context.Context, db gowild_data.Database, targetKind, targetID, projectID, author, body string) (*Comment, error) {
	if err := validActor(author); err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, validationf("comment body is required")
	}
	now := s.Now()
	c := &Comment{
		ID: newID(), TargetKind: targetKind, TargetID: targetID, Author: author,
		Kind: CommentKindComment, Body: body, Mentions: joinMentions(ExtractMentions(body)), CreatedAt: now,
	}
	if err := db.Table(Comment{}).Insert(ctx, c); err != nil {
		return nil, err
	}
	room := RoomItem
	if targetKind == TargetPost {
		room = RoomPost
	}
	if err := s.recordMentions(ctx, db, body, room, targetID, projectID, "comment", c.ID, author, now); err != nil {
		return nil, err
	}
	if author != ActorOwner {
		if err := s.markAnswered(ctx, db, author, room, targetID, now); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// ------------------------------------------------------------------- posts

// PostInput is what a new board post takes.
type PostInput struct {
	Author string
	Title  string
	Body   string
	Pinned bool
}

// PostPatch is a partial edit; nil leaves a field alone.
type PostPatch struct {
	Title  *string
	Body   *string
	Pinned *bool
}

// CreatePost adds a post to the project's board. Only the owner pins.
func (s *Service) CreatePost(ctx context.Context, projectKey string, in PostInput) (*Post, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	if err := validActor(in.Author); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, validationf("post title is required")
	}
	if in.Pinned && in.Author != ActorOwner {
		return nil, forbiddenf("only the owner pins")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.projectByKey(ctx, db, projectKey)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	post := &Post{
		ID: newID(), ProjectID: p.ID, Author: in.Author, Title: strings.TrimSpace(in.Title),
		Body: strings.TrimSpace(in.Body), Pinned: in.Pinned,
		Mentions: joinMentions(ExtractMentions(in.Body)), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, err
	}
	if err := s.recordMentions(ctx, db, in.Body, RoomPost, post.ID, p.ID, "post", post.ID, in.Author, now); err != nil {
		return nil, err
	}
	return post, nil
}

// ListPosts is the board: pinned first, then by latest activity.
func (s *Service) ListPosts(ctx context.Context, projectKey string) ([]*Post, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	p, err := s.projectByKey(ctx, db, projectKey)
	if err != nil {
		return nil, err
	}
	rows, err := gowild_dbx.All[Post](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"project_id": p.ID}})
	if err != nil {
		return nil, err
	}
	sortPosts(rows)
	return rows, nil
}

func postActivity(p *Post) time.Time {
	if p.LastReplyAt.After(p.CreatedAt) {
		return p.LastReplyAt
	}
	return p.CreatedAt
}

func sortPosts(rows []*Post) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return postActivity(a).After(postActivity(b))
	})
}

// PinnedPosts is the project's standing context, oldest first.
func (s *Service) PinnedPosts(ctx context.Context, projectID string) ([]*Post, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.pinnedPosts(ctx, db, projectID)
}

func (s *Service) pinnedPosts(ctx context.Context, db gowild_data.Database, projectID string) ([]*Post, error) {
	rows, err := gowild_dbx.All[Post](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"project_id": projectID, "pinned": true},
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	return rows, nil
}

// GetPost reads a post and its project.
func (s *Service) GetPost(ctx context.Context, id string) (*Post, *Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, nil, err
	}
	return s.postByID(ctx, db, id)
}

func (s *Service) postByID(ctx context.Context, db gowild_data.Database, id string) (*Post, *Project, error) {
	post, err := gowild_dbx.Get[Post](ctx, db, id)
	if err != nil {
		return nil, nil, err
	}
	if post == nil {
		return nil, nil, fmt.Errorf("%w: post %s", ErrNotFound, id)
	}
	p, err := s.projectByID(ctx, db, post.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return post, p, nil
}

// UpdatePost edits a post: the author or the owner, and only the owner pins.
func (s *Service) UpdatePost(ctx context.Context, id, actor string, patch PostPatch) (*Post, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	post, _, err := s.postByID(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if actor != ActorOwner && actor != post.Author {
		return nil, forbiddenf("only %s or the owner edits this post", post.Author)
	}
	if patch.Pinned != nil {
		if actor != ActorOwner {
			return nil, forbiddenf("only the owner pins")
		}
		post.Pinned = *patch.Pinned
	}
	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return nil, validationf("post title is required")
		}
		post.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Body != nil {
		post.Body = strings.TrimSpace(*patch.Body)
		post.Mentions = joinMentions(ExtractMentions(post.Body))
	}
	post.UpdatedAt = s.Now()
	if err := db.Table(Post{}).Update(ctx, post); err != nil {
		return nil, err
	}
	return post, nil
}

// PostReplies is a post's thread, oldest first.
func (s *Service) PostReplies(ctx context.Context, postID string) ([]*Comment, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.comments(ctx, db, TargetPost, postID)
}

// ReplyToPost appends to a post's thread.
func (s *Service) ReplyToPost(ctx context.Context, postID, author, body string) (*Comment, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	post, p, err := s.postByID(ctx, db, postID)
	if err != nil {
		return nil, err
	}
	c, err := s.addComment(ctx, db, TargetPost, post.ID, p.ID, author, body)
	if err != nil {
		return nil, err
	}
	post.ReplyCount++
	post.LastReplyAt = c.CreatedAt
	if err := db.Table(Post{}).Update(ctx, post); err != nil {
		return nil, err
	}
	return c, nil
}

// -------------------------------------------------------------------- chat

// roomProjectID resolves a chat room: "" is the general room.
func (s *Service) roomProjectID(ctx context.Context, db gowild_data.Database, projectKey string) (string, error) {
	if strings.TrimSpace(projectKey) == "" {
		return "", nil
	}
	p, err := s.projectByKey(ctx, db, projectKey)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

// PostChat says something in a project's room, or the general room for "".
// The line records the @names in it, and from an agent it answers every
// open mention of that agent in the room: the agent spoke there.
func (s *Service) PostChat(ctx context.Context, projectKey, author, body string) (*ChatMessage, error) {
	return s.postChat(ctx, projectKey, author, body, false)
}

// PostNotice puts a line in a room on the author's behalf without the
// author having spoken: the runner's pulses ("claude started EA-12 — Fix
// the poller"). A notice names no one, whatever the body quotes, and
// answers no one: the author's open mentions in the room stay open for the
// next check-in.
func (s *Service) PostNotice(ctx context.Context, projectKey, author, body string) (*ChatMessage, error) {
	return s.postChat(ctx, projectKey, author, body, true)
}

func (s *Service) postChat(ctx context.Context, projectKey, author, body string, notice bool) (*ChatMessage, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	if err := validActor(author); err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, validationf("message body is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID, err := s.roomProjectID(ctx, db, projectKey)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	m := &ChatMessage{ID: newID(), ProjectID: projectID, Author: author, Body: body, CreatedAt: now}
	if !notice {
		m.Mentions = joinMentions(ExtractMentions(body))
	}
	if err := db.Table(ChatMessage{}).Insert(ctx, m); err != nil {
		return nil, err
	}
	if notice {
		return m, nil
	}
	if err := s.recordMentions(ctx, db, body, RoomChat, projectID, projectID, "chat", m.ID, author, now); err != nil {
		return nil, err
	}
	if author != ActorOwner {
		if err := s.markAnswered(ctx, db, author, RoomChat, projectID, now); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ListChat returns a room's messages oldest first: the last limit of them,
// or everything after the message afterID when given (for polling).
func (s *Service) ListChat(ctx context.Context, projectKey, afterID string, limit int) ([]*ChatMessage, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	projectID, err := s.roomProjectID(ctx, db, projectKey)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.roomMessages(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	if afterID != "" {
		var anchor time.Time
		found := false
		for _, m := range rows {
			if m.ID == afterID {
				anchor, found = m.CreatedAt, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: message %s", ErrNotFound, afterID)
		}
		kept := rows[:0]
		for _, m := range rows {
			if m.CreatedAt.After(anchor) {
				kept = append(kept, m)
			}
		}
		rows = kept
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return rows, nil
	}
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

// RecentChat is the last n messages of a room by project id, oldest first.
func (s *Service) RecentChat(ctx context.Context, projectID string, n int) ([]*ChatMessage, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.recentChat(ctx, db, projectID, n)
}

func (s *Service) recentChat(ctx context.Context, db gowild_data.Database, projectID string, n int) ([]*ChatMessage, error) {
	rows, err := s.roomMessages(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	if n > 0 && len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return rows, nil
}

func (s *Service) roomMessages(ctx context.Context, db gowild_data.Database, projectID string) ([]*ChatMessage, error) {
	rows, err := gowild_dbx.All[ChatMessage](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"project_id": projectID}})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	return rows, nil
}

// ---------------------------------------------------------------- mentions

// recordMentions writes one Mention row per @name in body, except the
// author mentioning themself. Unknown names are recorded too: the queue
// only ever serves rows to the agent they name, so a stray @bob is inert.
func (s *Service) recordMentions(ctx context.Context, db gowild_data.Database, body, room, roomID, projectID, sourceKind, sourceID, author string, now time.Time) error {
	for _, name := range ExtractMentions(body) {
		if name == author || name == ActorOwner {
			continue
		}
		m := &Mention{
			ID: newID(), Agent: name, Room: room, RoomID: roomID, ProjectID: projectID,
			SourceKind: sourceKind, SourceID: sourceID, Author: author, CreatedAt: now,
		}
		if err := db.Table(Mention{}).Insert(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// markAnswered closes every open mention of agent in the room: the agent
// spoke there, which is the answer.
func (s *Service) markAnswered(ctx context.Context, db gowild_data.Database, agent, room, roomID string, now time.Time) error {
	rows, err := gowild_dbx.All[Mention](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"agent": agent, "room": room, "room_id": roomID},
	})
	if err != nil {
		return err
	}
	for _, m := range rows {
		if !m.AnsweredAt.IsZero() {
			continue
		}
		m.AnsweredAt = now
		if err := db.Table(Mention{}).Update(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// MarkMentionAnswered closes one mention explicitly — the runner's call
// when a respond job ends, answered or given up.
func (s *Service) MarkMentionAnswered(ctx context.Context, id string) (*Mention, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := gowild_dbx.Get[Mention](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("%w: mention %s", ErrNotFound, id)
	}
	if m.AnsweredAt.IsZero() {
		m.AnsweredAt = s.Now()
		if err := db.Table(Mention{}).Update(ctx, m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// GetMention reads one mention: the runner names it when it records a
// respond job's run.
func (s *Service) GetMention(ctx context.Context, id string) (*Mention, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	m, err := gowild_dbx.Get[Mention](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("%w: mention %s", ErrNotFound, id)
	}
	return m, nil
}

// UnansweredMentions lists an agent's open mentions, oldest first, leaving
// out the ones handed out MaxMentionAttempts times already.
func (s *Service) UnansweredMentions(ctx context.Context, agent string) ([]*Mention, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.unansweredMentions(ctx, db, agent)
}

func (s *Service) unansweredMentions(ctx context.Context, db gowild_data.Database, agent string) ([]*Mention, error) {
	rows, err := gowild_dbx.All[Mention](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"agent": agent}})
	if err != nil {
		return nil, err
	}
	kept := rows[:0]
	for _, m := range rows {
		if m.AnsweredAt.IsZero() && m.Attempts < MaxMentionAttempts {
			kept = append(kept, m)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt.Before(kept[j].CreatedAt) })
	return kept, nil
}
