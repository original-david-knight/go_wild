package tools

// CompanyKnowledgeSearchInput searches shared company knowledge.
type CompanyKnowledgeSearchInput struct {
	Query string `json:"query" description:"Free-text search query (title/content/tags). Leave empty to list recent entries."`
	Kind  string `json:"kind" description:"Optional knowledge category filter (e.g., strategy, policy, process)."`
	Limit int    `json:"limit" description:"Maximum entries to return (default 20)."`
}

// CompanyKnowledgeAddInput adds a shared company knowledge entry.
type CompanyKnowledgeAddInput struct {
	Kind     string         `json:"kind" description:"Knowledge category (for example strategy, policy, process)."`
	Title    string         `json:"title" description:"Short title for the entry."`
	Content  string         `json:"content" description:"Entry body/content." required:"true"`
	Tags     []string       `json:"tags" description:"Optional tags for filtering and retrieval."`
	Metadata map[string]any `json:"metadata" description:"Optional structured metadata object."`
}

// CompanyKnowledgeGetInput fetches one shared knowledge entry by ID.
type CompanyKnowledgeGetInput struct {
	EntryID string `json:"entry_id" description:"Knowledge entry ID." required:"true"`
}

// CompanyKnowledgeUpdateInput updates a shared knowledge entry.
type CompanyKnowledgeUpdateInput struct {
	EntryID  string         `json:"entry_id" description:"Knowledge entry ID." required:"true"`
	Kind     string         `json:"kind" description:"Updated category."`
	Title    string         `json:"title" description:"Updated title."`
	Content  string         `json:"content" description:"Updated content."`
	Tags     []string       `json:"tags" description:"Replace tags list. Pass empty list to clear."`
	Metadata map[string]any `json:"metadata" description:"Replace metadata object. Pass empty object to clear."`
}

// CompanyKnowledgeDeleteInput deletes a shared knowledge entry.
type CompanyKnowledgeDeleteInput struct {
	EntryID string `json:"entry_id" description:"Knowledge entry ID." required:"true"`
}
