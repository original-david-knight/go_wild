package gowild_data

import (
	"context"
)

// Database is the interface for database operations.
type Database interface {
	// AddTable registers a model type with the database, creating the table if needed.
	AddTable(model any) error

	// Table returns a TableDAO for the given model type.
	Table(model any) TableDAO

	// ForUser returns a user-scoped database view.
	ForUser(userID string) UserDatabase

	// RunInTransaction executes a function within a transaction.
	RunInTransaction(ctx context.Context, fn func(tx Database) error) error

	// Close closes the database connection.
	Close() error
}

// UserDatabase provides user-scoped table access.
type UserDatabase interface {
	// Table returns a user-scoped TableDAO for the given model type.
	Table(model any) TableDAO
}

// TableDAO provides CRUD operations for a specific table.
type TableDAO interface {
	// Insert adds a new record.
	Insert(ctx context.Context, model any) error

	// Update modifies an existing record.
	Update(ctx context.Context, model any) error

	// Delete removes a record by ID.
	Delete(ctx context.Context, id string) error

	// Get retrieves a record by ID.
	Get(ctx context.Context, id string, dest any) error

	// GetAll retrieves all records.
	GetAll(ctx context.Context) ([]any, error)

	// Query retrieves records matching the given conditions.
	Query(ctx context.Context, opts QueryOpts) ([]any, error)
}

// QueryOpts specifies query options.
type QueryOpts struct {
	// Where conditions as field=value pairs.
	Where map[string]any

	// WhereIn conditions as field IN (values) pairs.
	WhereIn map[string][]any

	// OrderBy specifies the field to order by.
	OrderBy string

	// OrderDesc reverses the sort order.
	OrderDesc bool

	// Limit restricts the number of results.
	Limit int

	// Offset skips the first N results.
	Offset int
}
