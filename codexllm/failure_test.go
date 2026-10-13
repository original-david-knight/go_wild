package codexllm

import (
	"context"
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		reason string
		want   error
	}{
		{"Selected model is at capacity. Please try a different model.", ErrBusy},
		{"We're currently experiencing high demand, which may cause temporary errors.", ErrBusy},
		{"exceeded retry limit, last status: 429 Too Many Requests", ErrBusy},
		{"You've hit your usage limit. Upgrade to Pro or try again at 3:05 PM.", ErrUsageLimit},
		{"apply_patch verification failed", nil},
		{"", nil},
	}
	for _, tc := range cases {
		if got := classify(tc.reason); got != tc.want {
			t.Errorf("classify(%q) = %v, want %v", tc.reason, got, tc.want)
		}
	}
}

// A run turned away for load is ErrBusy to errors.Is, and still the exit
// error underneath; a run that failed for another reason is neither.
func TestGenerate_BusyRunIsErrBusy(t *testing.T) {
	stageFakeCodex(t, `#!/bin/sh
echo '{"type":"turn.failed","error":{"message":"Selected model is at capacity. Please try a different model."}}'
exit 1
`)
	_, err := (&Client{Label: "test"}).Generate(context.Background(), "prompt", "")
	if !errors.Is(err, ErrBusy) || errors.Is(err, ErrUsageLimit) {
		t.Fatalf("Generate() error = %v, want ErrBusy alone", err)
	}
	var run *RunError
	if !errors.As(err, &run) || run.Exit == nil {
		t.Fatalf("Generate() error = %#v, want a RunError carrying the exit error", err)
	}

	stageFakeCodex(t, `#!/bin/sh
echo '{"type":"turn.failed","error":{"message":"something else broke"}}'
exit 1
`)
	_, err = (&Client{Label: "test"}).Generate(context.Background(), "prompt", "")
	if err == nil || errors.Is(err, ErrBusy) || errors.Is(err, ErrUsageLimit) {
		t.Fatalf("Generate() error = %v, want an error that is neither busy nor a usage limit", err)
	}
}
