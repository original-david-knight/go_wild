package tools

import (
	"testing"
)

func TestCollectRecipients(t *testing.T) {
	tests := []struct {
		to, cc, bcc []string
		expected    int
	}{
		{[]string{"a@b.com"}, nil, nil, 1},
		{[]string{"a@b.com"}, []string{"c@d.com"}, nil, 2},
		{[]string{"a@b.com"}, []string{"c@d.com"}, []string{"e@f.com"}, 3},
		{nil, nil, nil, 0},
		{[]string{"a@b.com", "x@y.com"}, []string{"c@d.com"}, nil, 3},
	}

	for _, tc := range tests {
		got := collectRecipients(tc.to, tc.cc, tc.bcc)
		if len(got) != tc.expected {
			t.Errorf("collectRecipients(%v, %v, %v) = %d recipients, want %d",
				tc.to, tc.cc, tc.bcc, len(got), tc.expected)
		}
	}
}

func TestCollectRecipients_Order(t *testing.T) {
	got := collectRecipients([]string{"a"}, []string{"b"}, []string{"c"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("expected [a b c], got %v", got)
	}
}

func TestTruncatePreview(t *testing.T) {
	// Short text
	if got := truncatePreview("hello"); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}

	// Exactly 100 chars
	s100 := make([]byte, 100)
	for i := range s100 {
		s100[i] = 'a'
	}
	if got := truncatePreview(string(s100)); got != string(s100) {
		t.Error("100-char string should not be truncated")
	}

	// Over 100 chars
	s200 := make([]byte, 200)
	for i := range s200 {
		s200[i] = 'b'
	}
	got := truncatePreview(string(s200))
	if len(got) != 103 { // 100 + "..."
		t.Errorf("expected 103 chars, got %d", len(got))
	}
	if got[100:] != "..." {
		t.Error("expected '...' suffix")
	}
}

func TestNewEmailOutbox(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")
	outbox := NewEmailOutbox(et, nil)
	if outbox == nil {
		t.Error("expected non-nil outbox")
	}
}
