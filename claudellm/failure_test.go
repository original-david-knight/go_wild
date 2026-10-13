package claudellm

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
		{"API Error: 529 Overloaded", ErrBusy},
		{`API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, ErrBusy},
		{`API Error: 429 {"type":"error","error":{"type":"rate_limit_error"}}`, ErrBusy},
		{"Claude AI usage limit reached|1760000000", ErrUsageLimit},
		{"You've hit your limit · resets 3pm (America/Los_Angeles)", ErrUsageLimit},
		{"Execution error", nil},
		{"", nil},
	}
	for _, tc := range cases {
		if got := classify(tc.reason); got != tc.want {
			t.Errorf("classify(%q) = %v, want %v", tc.reason, got, tc.want)
		}
	}
}

func TestGenerate_UsageLimitRunIsErrUsageLimit(t *testing.T) {
	stageFakeClaude(t, `#!/bin/sh
cat > /dev/null
echo '{"type":"result","is_error":true,"result":"Claude AI usage limit reached|1760000000"}'
exit 1
`)
	_, err := (&Client{Label: "test"}).Generate(context.Background(), "prompt", "")
	if !errors.Is(err, ErrUsageLimit) || errors.Is(err, ErrBusy) {
		t.Fatalf("Generate() error = %v, want ErrUsageLimit alone", err)
	}
}
