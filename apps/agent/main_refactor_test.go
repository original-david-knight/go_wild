package main

import (
	"os"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"short", 5, "short"},
		{"", 5, ""},
		{"with\nnewline", 20, "with newline"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestParseIntervalToMinutes(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"50m", 50, false},
		{"3h", 180, false},
		{"4d", 5760, false},
		{"1h30m", 90, false},
		{"2d12h", 3600, false},
		{"", 0, true},
		{"0m", 0, true},
		{"-5m", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := parseIntervalToMinutes(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseIntervalToMinutes(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseIntervalToMinutes(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestLooksLikeInterval(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"30m", true},
		{"2h", true},
		{"1d", true},
		{"1h30m", true},
		{"", false},
		{"abc", false},
		{"30", false},
		{"30x", false},
	}
	for _, tt := range tests {
		got := looksLikeInterval(tt.input)
		if got != tt.want {
			t.Errorf("looksLikeInterval(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestGetAgentID(t *testing.T) {
	// Save and restore env
	origEnv := os.Getenv("GOWILD_AGENT_ID")
	origFlag := *agentFlag
	defer func() {
		os.Setenv("GOWILD_AGENT_ID", origEnv)
		*agentFlag = origFlag
	}()

	// Default
	*agentFlag = ""
	os.Setenv("GOWILD_AGENT_ID", "")
	if got := getAgentID(); got != "jake" {
		t.Errorf("getAgentID() default = %q, want %q", got, "jake")
	}

	// From env
	os.Setenv("GOWILD_AGENT_ID", "Alice")
	if got := getAgentID(); got != "alice" {
		t.Errorf("getAgentID() env = %q, want %q", got, "alice")
	}

	// Flag takes precedence
	*agentFlag = "BOB"
	if got := getAgentID(); got != "bob" {
		t.Errorf("getAgentID() flag = %q, want %q", got, "bob")
	}
}

func TestCmdArg(t *testing.T) {
	cm := data.CommandMessage{
		Args: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
	}
	if got := cmdArg(cm, "key1"); got != "value1" {
		t.Errorf("cmdArg(key1) = %q, want %q", got, "value1")
	}
	if got := cmdArg(cm, "key2"); got != "" {
		t.Errorf("cmdArg(key2 int) = %q, want %q", got, "")
	}
	if got := cmdArg(cm, "missing"); got != "" {
		t.Errorf("cmdArg(missing) = %q, want %q", got, "")
	}
}

func TestParseTextCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantNil bool
	}{
		{"/help", "help", false},
		{"/addtask buy groceries", "addtask", false},
		{"/", "", true},
		{"/smart", "smart", false},
		{"/approve all", "approve", false},
	}
	for _, tt := range tests {
		got := parseTextCommand(tt.input)
		if tt.wantNil {
			if got != nil {
				t.Errorf("parseTextCommand(%q) = %+v, want nil", tt.input, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("parseTextCommand(%q) = nil, want command %q", tt.input, tt.wantCmd)
			continue
		}
		if got.Command != tt.wantCmd {
			t.Errorf("parseTextCommand(%q).Command = %q, want %q", tt.input, got.Command, tt.wantCmd)
		}
	}

	// Check args parsing
	cm := parseTextCommand("/addtask buy groceries")
	if cm == nil {
		t.Fatal("parseTextCommand(/addtask buy groceries) = nil")
	}
	if desc, _ := cm.Args["description"].(string); desc != "buy groceries" {
		t.Errorf("addtask description = %q, want %q", desc, "buy groceries")
	}

	cm = parseTextCommand("/approve all")
	if cm == nil {
		t.Fatal("parseTextCommand(/approve all) = nil")
	}
	if id, _ := cm.Args["id"].(string); id != "all" {
		t.Errorf("approve id = %q, want %q", id, "all")
	}

}

func TestStrVal(t *testing.T) {
	m := map[string]any{
		"name":  "alice",
		"count": 42,
	}
	if got := strVal(m, "name"); got != "alice" {
		t.Errorf("strVal(name) = %q, want %q", got, "alice")
	}
	if got := strVal(m, "missing"); got != "" {
		t.Errorf("strVal(missing) = %q, want %q", got, "")
	}
	if got := strVal(m, "count"); got != "" {
		t.Errorf("strVal(count int) = %q, want %q", got, "")
	}
	if got := strVal(nil, "key"); got != "" {
		t.Errorf("strVal(nil, key) = %q, want %q", got, "")
	}
}

func TestParseTextCommand_Telegram(t *testing.T) {
	cm := parseTextCommand("/telegram bot123token")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if cm.Command != "telegram" {
		t.Errorf("command = %q, want 'telegram'", cm.Command)
	}
	if v, _ := cm.Args["value"].(string); v != "bot123token" {
		t.Errorf("value = %q, want 'bot123token'", v)
	}

	// No value
	cm2 := parseTextCommand("/telegram")
	if cm2 == nil {
		t.Fatal("expected non-nil")
	}
	if _, ok := cm2.Args["value"]; ok {
		t.Error("expected no value arg when none provided")
	}
}

func TestParseTextCommand_Email(t *testing.T) {
	// /email apikey <key>
	cm := parseTextCommand("/email apikey mykey123")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if sub, _ := cm.Args["subcommand"].(string); sub != "apikey" {
		t.Errorf("subcommand = %q, want 'apikey'", sub)
	}
	if val, _ := cm.Args["value"].(string); val != "mykey123" {
		t.Errorf("value = %q, want 'mykey123'", val)
	}

	// /email <inbox_id>
	cm2 := parseTextCommand("/email inbox_abc")
	if cm2 == nil {
		t.Fatal("expected non-nil")
	}
	if val, _ := cm2.Args["value"].(string); val != "inbox_abc" {
		t.Errorf("value = %q, want 'inbox_abc'", val)
	}
	if _, ok := cm2.Args["subcommand"]; ok {
		t.Error("expected no subcommand for inbox ID")
	}

	// /email (no args)
	cm3 := parseTextCommand("/email")
	if cm3 == nil {
		t.Fatal("expected non-nil")
	}
	if _, ok := cm3.Args["value"]; ok {
		t.Error("expected no value arg when none provided")
	}
}

func TestParseTextCommand_AddRecurring(t *testing.T) {
	// With interval and description
	cm := parseTextCommand("/addrecurring 30m check email")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if cm.Command != "addrecurring" {
		t.Errorf("command = %q, want 'addrecurring'", cm.Command)
	}
	if interval, _ := cm.Args["interval"].(string); interval != "30m" {
		t.Errorf("interval = %q, want '30m'", interval)
	}
	if desc, _ := cm.Args["description"].(string); desc != "check email" {
		t.Errorf("description = %q, want 'check email'", desc)
	}

	// With description only (not an interval)
	cm2 := parseTextCommand("/addrecurring check email daily")
	if cm2 == nil {
		t.Fatal("expected non-nil")
	}
	if desc, _ := cm2.Args["description"].(string); desc != "check email daily" {
		t.Errorf("description = %q, want 'check email daily'", desc)
	}
	if _, ok := cm2.Args["interval"]; ok {
		t.Error("expected no interval when first word is not an interval")
	}
}

func TestParseTextCommand_DeleteRecurring(t *testing.T) {
	cm := parseTextCommand("/deleterecurring task_abc123")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if id, _ := cm.Args["id"].(string); id != "task_abc123" {
		t.Errorf("id = %q, want 'task_abc123'", id)
	}
}

func TestParseTextCommand_ImageAndFile(t *testing.T) {
	// /image with path
	cm := parseTextCommand("/image /data/screenshot.png")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if path, _ := cm.Args["path"].(string); path != "/data/screenshot.png" {
		t.Errorf("path = %q, want '/data/screenshot.png'", path)
	}

	// /file with path
	cm2 := parseTextCommand("/file /data/report.pdf")
	if cm2 == nil {
		t.Fatal("expected non-nil")
	}
	if path, _ := cm2.Args["path"].(string); path != "/data/report.pdf" {
		t.Errorf("path = %q, want '/data/report.pdf'", path)
	}

	// /f alias
	cm3 := parseTextCommand("/f /data/code.py")
	if cm3 == nil {
		t.Fatal("expected non-nil")
	}
	if cm3.Command != "f" {
		t.Errorf("command = %q, want 'f'", cm3.Command)
	}
	if path, _ := cm3.Args["path"].(string); path != "/data/code.py" {
		t.Errorf("path = %q, want '/data/code.py'", path)
	}
}

func TestParseTextCommand_Reject(t *testing.T) {
	cm := parseTextCommand("/reject 3")
	if cm == nil {
		t.Fatal("expected non-nil")
	}
	if id, _ := cm.Args["id"].(string); id != "3" {
		t.Errorf("id = %q, want '3'", id)
	}
}

func TestApplyAgentTemplate(t *testing.T) {
	tests := []struct {
		prompt string
		name   string
		want   string
	}{
		{"Hello {{AgentName}}", "Alice", "Hello Alice"},
		{"You are Jake.", "Bob", "You are Bob."},
		{"# You are Jake.", "Charlie", "# You are Charlie."},
		{"Jake_World_Modeler", "Eve", "Eve_World_Modeler"},
		{"No placeholders", "X", "No placeholders"},
	}
	for _, tt := range tests {
		got := applyAgentTemplate(tt.prompt, tt.name)
		if got != tt.want {
			t.Errorf("applyAgentTemplate(%q, %q) = %q, want %q", tt.prompt, tt.name, got, tt.want)
		}
	}
}
