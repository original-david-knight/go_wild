# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the package
go build ./...

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -run TestSqliteDatabase_CRUD -v
```

## Architecture

This is a Go package for auto-discovery data access. Library packages register their models at init() time, and applications call `AddAllTables(db)` to register all discovered tables.

### Core Components

**Registry** (`registry.go`) - Auto-discovery mechanism:
- `RegisterFunc()` - Register a table provider function
- `AddAllTables(db)` - Add all registered tables to a database
- Thread-safe with `sync.RWMutex`

**Database Interface** (`database.go`) - Abstract database operations:
- `Database` - Main interface with `AddTable()`, `Table()`, `ForUser()`, `RunInTransaction()`
- `UserDatabase` - User-scoped database view
- `TableDAO` - CRUD operations for a specific table
- `QueryOpts` - Query filtering, ordering, pagination

**Model Reflection** (`model.go`) - Schema generation from structs (internal):
- `getModelMeta()` - Extract metadata via reflection
- `createTableSQL()` / `createTableSQLPostgres()` - Generate CREATE TABLE statements
- Automatic type mapping (string→TEXT, int→INTEGER, etc.)
- JSON serialization for slices, maps, and nested structs

**SQLite Implementation** (`sqlite.go`) - Concrete database backend:
- `NewSqliteDatabase(dsn)` - Create connection (use `:memory:` for tests)
- Standard CRUD operations via `TableDAO` interface
- User-scoped queries via `ForUser(userID)` (filters by `user_id`)
- Transaction support with `RunInTransaction()`

**PostgreSQL Implementation** (`postgres.go`) - Production database backend:
- `NewPostgresDatabase(connString)` - Create connection via `pgx/v5`
- Standard CRUD operations (uses `$1` placeholders)
- User-scoped queries via `ForUser(userID)`
- Used by `apps/agent_manager` for production deployments

### Auto-Discovery Pattern

Library packages register in `init()`:

```go
// mypackage/data.go
func init() {
    gowild_data.RegisterFunc(func(db gowild_data.Database) {
        db.AddTable(MyModel{})
    })
}
```

Applications import and call registry:

```go
import (
    _ "myapp/calendar"  // Triggers init(), registers tables
    _ "myapp/tasks"
)

func main() {
    db, _ := gowild_data.NewSqliteDatabase("app.db")
    gowild_data.AddAllTables(db)  // Creates all tables
}
```

### Model Definition

Models are structs with `json` tags:

```go
type Task struct {
    ID        string    `json:"id"`        // Primary key
    UserID    string    `json:"user_id"`   // For user-scoped queries
    Title     string    `json:"title"`
    Completed bool      `json:"completed"`
    Tags      []string  `json:"tags"`      // Stored as JSON
    DueDate   time.Time `json:"due_date"`  // Stored as RFC3339
}
```

### Type Mappings

| Go Type | SQLite Type |
|---------|-------------|
| string | TEXT |
| int, int64 | INTEGER |
| float64 | REAL |
| bool | INTEGER (0/1) |
| time.Time | TEXT (RFC3339) |
| []T, map[K]V | TEXT (JSON) |

### User-Scoped Data

For multi-tenant applications:

```go
userDAO := db.ForUser("user-123").Table(Task{})
userDAO.Insert(ctx, task)  // Sets user_id automatically
userDAO.GetAll(ctx)        // Only returns user-123's data
```

## Key Dependencies

- `github.com/mattn/go-sqlite3` v1.14.24 - SQLite driver
- `github.com/jackc/pgx/v5` - PostgreSQL driver

## Testing

Use in-memory database:

```go
db, _ := gowild_data.NewSqliteDatabase(":memory:")
defer db.Close()
```

