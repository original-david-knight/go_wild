# gowild_data

Auto-discovery data access for Go packages.

## How It Works

The package uses Go's `init()` functions for auto-registration at import time:

```
App imports library package
    ↓
Go runs library's init() function
    ↓
init() calls gowild_data.RegisterFunc()
    ↓
Table provider added to global registry
    ↓
App calls gowild_data.AddAllTables(db)
    ↓
All registered providers add their tables
```

## Usage

### In Library Packages

Register your tables in an `init()` function:

```go
// mypackage/data.go
package mypackage

import "github.com/anthropics/wilder/golang/gowild_data"

// CalendarEvent is the model
type CalendarEvent struct {
    ID        string `json:"id"`
    UserID    string `json:"user_id"`
    Title     string `json:"title"`
    StartTime string `json:"start_time"`
}

// Register tables at package init time
func init() {
    gowild_data.RegisterFunc(func(db gowild_data.Database) {
        db.AddTable(CalendarEvent{})
    })
}
```

### In Applications

Import your library packages and call the registry:

```go
package main

import (
    "github.com/anthropics/wilder/golang/gowild_data"

    // Importing these packages triggers their init() functions
    _ "myapp/calendar"
    _ "myapp/tasks"
    _ "myapp/notes"
)

func main() {
    // Create database
    db, err := gowild_data.NewSqliteDatabase("app.db")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Add all registered tables from all imported packages
    gowild_data.AddAllTables(db)

    // Use the database...
}
```

## Model Definition

Models are regular Go structs. The package uses reflection to generate schemas:

```go
type Task struct {
    ID          string    `json:"id"`           // Primary key (field named "ID" or tagged)
    UserID      string    `json:"user_id"`      // For user-scoped queries
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Priority    int       `json:"priority"`
    Completed   bool      `json:"completed"`
    Tags        []string  `json:"tags"`         // Stored as JSON
    DueDate     time.Time `json:"due_date"`     // Stored as RFC3339
}
```

### Type Mappings

| Go Type | SQLite Type |
|---------|-------------|
| string | TEXT |
| int, int64, etc. | INTEGER |
| float32, float64 | REAL |
| bool | INTEGER (0/1) |
| time.Time | TEXT (RFC3339) |
| []T, map[K]V | TEXT (JSON) |
| struct | TEXT (JSON) |

## Database Operations

### Basic CRUD

```go
ctx := context.Background()
dao := db.Table(Task{})

// Insert
task := &Task{ID: "task-1", Title: "Buy groceries"}
dao.Insert(ctx, task)

// Get
var retrieved Task
dao.Get(ctx, "task-1", &retrieved)

// Update
task.Completed = true
dao.Update(ctx, task)

// Delete
dao.Delete(ctx, "task-1")
```

### Conditional writes

The `data/dbx` helpers `InsertNew`, `UpdateIf`, and `DeleteIf` use the SQL
drivers' optional `AtomicTableDAO` capability. Each makes its decision in one
SQL statement. `InsertNew` returns false for an existing primary key;
`UpdateIf` and `DeleteIf` return false for a missing row or a failed condition.
They never implement a conditional write with a separate read and write.

```go
// row.Revision is the new revision; base is the revision previously read.
updated, err := dbx.UpdateIf(ctx, db, row, map[string]any{"revision": base})
deleted, err := dbx.DeleteIf[Task](ctx, db, row.ID, map[string]any{"revision": base})
```

Conditions name registered database columns. Values use the same encoding as
writes, including timestamps and booleans. `nil` matches SQL NULL. A zero
value for a non-pointer field also matches SQL NULL, consistent with reads
of nullable columns added to historical rows. Other unique-constraint errors
remain errors; they do not count as an existing primary key.

The capability is available on ordinary, transaction, and user-scoped tables.
Adapters wrapping `TableDAO` must also forward `AtomicTableDAO` to use these
helpers; otherwise the helpers return `dbx.ErrAtomicUnsupported`. Use a
transaction as well when several writes must commit or roll back together.

### Querying

```go
// Get all
results, _ := dao.GetAll(ctx)

// Query with conditions
results, _ := dao.Query(ctx, gowild_data.QueryOpts{
    Where:     map[string]any{"completed": false},
    OrderBy:   "due_date",
    OrderDesc: false,
    Limit:     10,
})
```

### User-Scoped Data

For multi-tenant applications:

```go
// Get user-scoped DAO
userDAO := db.ForUser("user-123").Table(Task{})

// All operations are automatically filtered by user_id
userDAO.Insert(ctx, task)  // Sets user_id = "user-123"
userDAO.GetAll(ctx)        // Only returns user-123's tasks
```

### Transactions

```go
err := db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
    taskDAO := tx.Table(Task{})
    taskDAO.Insert(ctx, task1)
    taskDAO.Insert(ctx, task2)
    return nil  // Commit
    // Return error to rollback
})
```

## Custom Table Names

Implement `TableNamer` to customize:

```go
type CalendarEvent struct {
    // fields...
}

func (CalendarEvent) TableName() string {
    return "calendar_events"  // Instead of "calendar_events" (default)
}
```

## Testing

Use in-memory database for tests:

```go
func TestMyFeature(t *testing.T) {
    db, _ := gowild_data.NewSqliteDatabase(":memory:")
    defer db.Close()

    db.AddTable(Task{})
    // Test...
}
```
