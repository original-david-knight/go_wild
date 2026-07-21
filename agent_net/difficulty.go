package gowild_agent_net

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// DifficultyManager manages dynamic PoW difficulty based on network load.
type DifficultyManager struct {
	db             gowild_data.Database
	baseDifficulty int
}

// NewDifficultyManager creates a new difficulty manager.
func NewDifficultyManager(db gowild_data.Database, baseDifficulty int) *DifficultyManager {
	if baseDifficulty <= 0 {
		baseDifficulty = DefaultBaseDifficulty
	}
	return &DifficultyManager{
		db:             db,
		baseDifficulty: baseDifficulty,
	}
}

// GetCurrentDifficulty returns the current PoW difficulty based on network load.
func (d *DifficultyManager) GetCurrentDifficulty(ctx context.Context) (*PoWDifficulty, error) {
	postsLastHour, err := d.countPostsLastHour(ctx)
	if err != nil {
		// On error, return base difficulty
		return &PoWDifficulty{
			BaseDifficulty:    d.baseDifficulty,
			CurrentDifficulty: d.baseDifficulty,
			PostsLastHour:     0,
		}, nil
	}

	currentDifficulty := d.baseDifficulty
	if postsLastHour > 10000 {
		currentDifficulty = d.baseDifficulty + 2
	} else if postsLastHour > 5000 {
		currentDifficulty = d.baseDifficulty + 1
	}

	return &PoWDifficulty{
		BaseDifficulty:    d.baseDifficulty,
		CurrentDifficulty: currentDifficulty,
		PostsLastHour:     postsLastHour,
	}, nil
}

// countPostsLastHour counts posts created in the last hour.
func (d *DifficultyManager) countPostsLastHour(ctx context.Context) (int, error) {
	results, err := d.db.Table(Post{}).GetAll(ctx)
	if err != nil {
		return 0, err
	}

	oneHourAgo := time.Now().Add(-time.Hour)
	count := 0

	for _, result := range results {
		post, ok := result.(*Post)
		if !ok {
			continue
		}
		if post.CreatedAt.After(oneHourAgo) {
			count++
		}
	}

	return count, nil
}
