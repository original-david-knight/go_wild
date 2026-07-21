package data

import "time"

// AgentSite represents a published static website.
type AgentSite struct {
	ID        string    `json:"id"`         // slug = primary key
	AgentID   string    `json:"agent_id"`   // owning agent pubkey
	Title     string    `json:"title"`
	FileCount int       `json:"file_count"`
	TotalSize int64     `json:"total_size"`
	Status    string    `json:"status"` // "active"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
