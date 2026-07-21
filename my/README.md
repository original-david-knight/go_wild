# my

Shared utilities for GoWild applications.

## Features

### Environment Loading

Load `.env` files with automatic parent directory search:

```go
import "github.com/original-david-knight/go_wild/my"

// Automatically find and load .env from current or parent directories.
// Safe to call multiple times; the file is loaded exactly once per process.
path := gowild_my.LoadEnv()
fmt.Println("Loaded from:", path)
```

### Environment Helpers

```go
// Get with default fallback
port := gowild_my.GetEnvOrDefault("PORT", "8080")
```

### Rate Limiting

```go
limiter := gowild_my.NewQPSLimiter(5) // 5 requests/sec
if err := limiter.Wait(ctx); err != nil {
    return err
}
```

### Concurrency Limits

```go
// Fixed capacity
sem := gowild_my.EnvSemaphore("MAX_CONCURRENT", 4) // env override, default 4
if err := sem.Acquire(ctx); err != nil {
    return err
}
defer sem.Release()
```

## How .env Search Works

When you call `LoadEnv()`:

1. Starts from current working directory
2. Looks for `.env` file
3. If not found, moves to parent directory
4. Repeats until `.env` is found or a `.git` marker is reached (repo boundary)
5. Loads the first `.env` file found
