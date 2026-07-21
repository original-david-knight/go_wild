package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// CreatePostRequest represents the request body for creating a legacy text post.
type CreatePostRequest struct {
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PostResponse represents the response for any post type.
type PostResponse struct {
	ID                 string         `json:"id"`
	PublicKey          string         `json:"public_key"`
	Content            string         `json:"content"`
	VerificationMethod string         `json:"verification_method"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          string         `json:"created_at"`
	// Isnad v2 fields
	PostType     string   `json:"post_type,omitempty"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Rating       *float64 `json:"rating,omitempty"`
	TargetPostID string   `json:"target_post_id,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	ClaimID      string   `json:"claim_id,omitempty"`
	AuthorPubkey string   `json:"author_pubkey,omitempty"`
	// v3: New fields
	RefID          string `json:"ref_id,omitempty"`
	Topic          string `json:"topic,omitempty"`
	RewardLamports int64  `json:"reward_lamports,omitempty"`
	Deadline       string `json:"deadline,omitempty"`
	Result         string `json:"result,omitempty"`
	Methodology    string `json:"methodology,omitempty"`
	// v3: Settlement fields
	Chain          string `json:"chain,omitempty"`
	TxHash         string `json:"tx_hash,omitempty"`
	AmountLamports int64  `json:"amount_lamports,omitempty"`
}

// CreatePostResponse is an alias for backward compatibility.
type CreatePostResponse = PostResponse

// TypedPayload is used to determine the post type from JSON.
type TypedPayload struct {
	Type string `json:"type"`
}

// HandleCreatePost handles POST /api/v1/posts.
// Supports three payload types: text (legacy), isnad_claim, isnad_endorsement.
func (h *Handlers) HandleCreatePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}

	// Get agent info from context
	agentID := GetAgentID(r.Context())
	agentPubkey := GetAgentPubkey(r.Context())
	tier := GetAgentTier(r.Context())
	verificationMethod := gowild_agent_net.VerificationMethodPoW
	if tier == gowild_agent_net.TierPremium {
		verificationMethod = gowild_agent_net.VerificationMethodPremium
	}

	// Read body from cache (already read by middleware)
	body := GetCachedBody(r.Context())
	if body == nil {
		writeBadRequest(w, "Request body is required")
		return
	}

	// Determine payload type
	var typed TypedPayload
	if err := json.Unmarshal(body, &typed); err != nil {
		writeBadRequest(w, "Invalid JSON: "+err.Error())
		return
	}

	var post *gowild_agent_net.Post
	var err error

	switch typed.Type {
	case gowild_agent_net.PostTypeIsnadClaim:
		post, err = h.handleIsnadClaim(r, body, agentPubkey, verificationMethod)
	case gowild_agent_net.PostTypeIsnadEndorsement:
		post, err = h.handleIsnadEndorsement(r, body, agentPubkey, verificationMethod)
	case gowild_agent_net.PostTypeIsnadVerification:
		post, err = h.handleIsnadVerification(r, body, agentPubkey, verificationMethod)
	case gowild_agent_net.PostTypeBounty:
		post, err = h.handleBounty(r, body, agentPubkey, verificationMethod)
	case gowild_agent_net.PostTypeSolution:
		post, err = h.handleSolution(r, body, agentPubkey, verificationMethod)
	case gowild_agent_net.PostTypeIsnadSettlement:
		post, err = h.handleSettlement(r, body, agentPubkey, verificationMethod)
	default:
		// Legacy text post
		post, err = h.handleTextPost(r, body, agentID, verificationMethod)
	}

	if err != nil {
		// Check if it's a validation error vs internal error
		writeBadRequest(w, err.Error())
		return
	}

	// Update last active for premium agents
	h.service.UpdateLastActive(r.Context(), agentID)

	writeJSON(w, http.StatusCreated, postToResponse(post))
}

// handleTextPost handles legacy text posts.
func (h *Handlers) handleTextPost(r *http.Request, body []byte, agentID, verificationMethod string) (*gowild_agent_net.Post, error) {
	var req CreatePostRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	if req.Content == "" {
		return nil, errContentRequired
	}

	post, err := h.service.CreatePost(r.Context(), agentID, req.Content, verificationMethod, req.Metadata)
	if err != nil {
		return nil, err
	}

	post.PostType = gowild_agent_net.PostTypeText
	return post, nil
}

// handleIsnadClaim handles isnad_claim payloads.
func (h *Handlers) handleIsnadClaim(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var claim gowild_agent_net.IsnadClaim
	if err := json.Unmarshal(body, &claim); err != nil {
		return nil, err
	}

	return h.service.CreateIsnadClaimPost(r.Context(), &claim, agentPubkey, verificationMethod)
}

// handleIsnadEndorsement handles isnad_endorsement payloads.
func (h *Handlers) handleIsnadEndorsement(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var endorsement gowild_agent_net.IsnadEndorsement
	if err := json.Unmarshal(body, &endorsement); err != nil {
		return nil, err
	}

	return h.service.CreateIsnadEndorsementPost(r.Context(), &endorsement, agentPubkey, verificationMethod)
}

// handleIsnadVerification handles isnad_verification payloads.
func (h *Handlers) handleIsnadVerification(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var verification gowild_agent_net.IsnadVerification
	if err := json.Unmarshal(body, &verification); err != nil {
		return nil, err
	}

	return h.service.CreateIsnadVerificationPost(r.Context(), &verification, agentPubkey, verificationMethod)
}

// handleBounty handles bounty payloads.
func (h *Handlers) handleBounty(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var bounty gowild_agent_net.BountyPost
	if err := json.Unmarshal(body, &bounty); err != nil {
		return nil, err
	}

	return h.service.CreateBountyPost(r.Context(), &bounty, agentPubkey, verificationMethod)
}

// handleSolution handles solution payloads.
func (h *Handlers) handleSolution(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var solution gowild_agent_net.SolutionPost
	if err := json.Unmarshal(body, &solution); err != nil {
		return nil, err
	}

	return h.service.CreateSolutionPost(r.Context(), &solution, agentPubkey, verificationMethod)
}

// handleSettlement handles settlement payloads.
func (h *Handlers) handleSettlement(r *http.Request, body []byte, agentPubkey []byte, verificationMethod string) (*gowild_agent_net.Post, error) {
	var settlement gowild_agent_net.SettlementPost
	if err := json.Unmarshal(body, &settlement); err != nil {
		return nil, err
	}

	return h.service.CreateSettlementPost(r.Context(), &settlement, agentPubkey, verificationMethod)
}

// postToResponse converts a Post to a PostResponse.
func postToResponse(p *gowild_agent_net.Post) PostResponse {
	return PostResponse{
		ID:                 p.ID,
		PublicKey:          p.PublicKey,
		Content:            p.Content,
		VerificationMethod: p.VerificationMethod,
		Metadata:           p.Metadata,
		CreatedAt:          p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		PostType:           p.PostType,
		Confidence:         p.Confidence,
		Rating:             p.Rating,
		TargetPostID:       p.TargetPostID,
		Tags:               p.Tags,
		ClaimID:            p.ClaimID,
		AuthorPubkey:       p.AuthorPubkey,
		// v3: New fields
		RefID:          p.RefID,
		Topic:          p.Topic,
		RewardLamports: p.RewardLamports,
		Deadline:       p.Deadline,
		Result:         p.Result,
		Methodology:    p.Methodology,
		Chain:          p.Chain,
		TxHash:         p.TxHash,
		AmountLamports: p.AmountLamports,
	}
}

var errContentRequired = &ValidationError{Message: "Content is required"}

// ValidationError represents a validation error.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// ListPostsResponse represents the response for listing posts.
type ListPostsResponse struct {
	Posts      []PostResponse `json:"posts"`
	NextOffset int            `json:"next_offset,omitempty"`
}

// HandleListPosts handles GET /api/v1/posts.
// Supports query parameters: type, min_confidence, min_rating, tag, author, limit, offset.
func (h *Handlers) HandleListPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	query := r.URL.Query()

	// Parse pagination
	limit := 20
	offset := 0

	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Build filter
	filter := gowild_agent_net.PostsFilter{
		Limit:  limit,
		Offset: offset,
	}

	// Parse Isnad filters
	if t := query.Get("type"); t != "" {
		filter.PostType = t
	}

	if mc := query.Get("min_confidence"); mc != "" {
		if parsed, err := strconv.ParseFloat(mc, 64); err == nil && parsed >= 0 && parsed <= 1 {
			filter.MinConfidence = &parsed
		}
	}

	if mr := query.Get("min_rating"); mr != "" {
		if parsed, err := strconv.ParseFloat(mr, 64); err == nil && parsed >= 0 && parsed <= 1 {
			filter.MinRating = &parsed
		}
	}

	if tag := query.Get("tag"); tag != "" {
		filter.Tag = tag
	}

	if author := query.Get("author"); author != "" {
		filter.Author = author
	}

	// v3: New query parameters
	if since := query.Get("since"); since != "" {
		if parsed, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = &parsed
		}
	}

	if refID := query.Get("ref_id"); refID != "" {
		filter.RefID = refID
	}

	if topic := query.Get("topic"); topic != "" {
		filter.Topic = topic
	}

	if result := query.Get("result"); result != "" {
		filter.Result = result
	}

	posts, err := h.service.ListPostsFiltered(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list posts: "+err.Error())
		return
	}

	resp := ListPostsResponse{
		Posts: make([]PostResponse, 0, len(posts)),
	}

	for _, p := range posts {
		resp.Posts = append(resp.Posts, postToResponse(&p))
	}

	if len(posts) == limit {
		resp.NextOffset = offset + limit
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetPost handles GET /api/v1/posts/{id}.
func (h *Handlers) HandleGetPost(w http.ResponseWriter, r *http.Request, postID string) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	post, err := h.service.GetPost(r.Context(), postID)
	if err != nil {
		writeNotFound(w, "Post not found")
		return
	}

	writeJSON(w, http.StatusOK, CreatePostResponse{
		ID:                 post.ID,
		PublicKey:          post.PublicKey,
		Content:            post.Content,
		VerificationMethod: post.VerificationMethod,
		Metadata:           post.Metadata,
		CreatedAt:          post.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}
