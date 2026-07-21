package main

import "testing"

func TestHeartbeatMessageFromInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantMsg string
		wantOK  bool
	}{
		{
			name:    "valid heartbeat",
			line:    `{"type":"heartbeat","message":"Check messages"}`,
			wantMsg: "Check messages",
			wantOK:  true,
		},
		{
			name:    "valid heartbeat trims whitespace",
			line:    "  {\"type\":\"heartbeat\",\"message\":\"  Ping  \"}  ",
			wantMsg: "Ping",
			wantOK:  true,
		},
		{
			name:   "empty heartbeat message",
			line:   `{"type":"heartbeat","message":"   "}`,
			wantOK: false,
		},
		{
			name:   "non heartbeat json",
			line:   `{"type":"command","command":"help"}`,
			wantOK: false,
		},
		{
			name:   "plain text",
			line:   "hello",
			wantOK: false,
		},
		{
			name:   "invalid json",
			line:   `{"type":"heartbeat"`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMsg, gotOK := heartbeatMessageFromInput(tt.line)
			if gotOK != tt.wantOK {
				t.Fatalf("got ok=%v, want %v", gotOK, tt.wantOK)
			}
			if gotMsg != tt.wantMsg {
				t.Fatalf("got message=%q, want %q", gotMsg, tt.wantMsg)
			}
		})
	}
}
