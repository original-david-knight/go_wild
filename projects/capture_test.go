package gowild_projects

import (
	"errors"
	"sync"
	"testing"
)

func TestCaptureIdentitySurvivesReplayAndDeletion(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.workers()
	in := ItemInput{ID: "source-review-1", Origin: "from: GitHub review acme/api#1", Title: "Review", Type: TypeCodeReview, PRURL: reviewedPR}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.s.CreateItem(f.ctx, "EA", in)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d copies", created)
	}
	it, p, err := f.s.GetItem(f.ctx, in.ID)
	if err != nil || it.Origin != in.Origin || it.Number != 1 || p.NextNumber != 2 {
		t.Fatalf("captured item / counter: %+v, %+v, %v", it, p, err)
	}
	it.Deleted = true
	db, _ := f.s.database()
	if err := db.Table(Item{}).Update(f.ctx, it); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", in); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay restored a deleted capture: %v", err)
	}
	next := f.item("EA", "Next")
	if next.Number != 2 {
		t.Fatalf("replays consumed item numbers: %d", next.Number)
	}
}
