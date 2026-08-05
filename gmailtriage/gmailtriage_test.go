package gmailtriage

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// recordingMailbox is a Mailbox that answers from a fixture and remembers
// every call. `refuse`, when set, is what a policy-enforcing implementation
// returns instead of performing the write.
type recordingMailbox struct {
	messages []Message
	calls    []string
	refuse   error
}

func (m *recordingMailbox) ListMessages(_ context.Context, account, query string, max int64) ([]Message, error) {
	m.calls = append(m.calls, "list "+account+" q="+query)
	return m.messages, nil
}

func (m *recordingMailbox) GetMessage(_ context.Context, account, id string) (*Message, error) {
	m.calls = append(m.calls, "get "+account+" "+id)
	for i := range m.messages {
		if m.messages[i].ID == id {
			return &m.messages[i], nil
		}
	}
	return nil, errors.New("no such message")
}

func (m *recordingMailbox) ApplyLabel(_ context.Context, account, messageID, label string) error {
	if m.refuse != nil {
		return m.refuse
	}
	m.calls = append(m.calls, "apply "+account+" "+messageID+" "+label)
	return nil
}

func (m *recordingMailbox) RemoveLabel(_ context.Context, account, messageID, label string) error {
	if m.refuse != nil {
		return m.refuse
	}
	m.calls = append(m.calls, "remove "+account+" "+messageID+" "+label)
	return nil
}

func (m *recordingMailbox) Archive(_ context.Context, account, messageID string) error {
	if m.refuse != nil {
		return m.refuse
	}
	m.calls = append(m.calls, "archive "+account+" "+messageID)
	return nil
}

func fixture() *recordingMailbox {
	return &recordingMailbox{messages: []Message{
		{ID: "m-1", ThreadID: "t-1", From: "news@example.com", Subject: "Weekly digest", Snippet: "Ten things"},
		{ID: "m-2", ThreadID: "t-2", From: "landlord@example.com", Subject: "Lease renewal", Snippet: "Please confirm"},
	}}
}

func TestGroupRegistersExactlyTheTriageVerbs(t *testing.T) {
	tools := Tools(fixture())

	var got []string
	for _, tool := range tools {
		got = append(got, tool.Name())
	}
	slices.Sort(got)
	want := slices.Clone(ToolNames)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}

	for _, tool := range tools {
		if strings.TrimSpace(tool.Description()) == "" {
			t.Errorf("tool %s has no description", tool.Name())
		}
	}
}

// TestNoSendVerbExistsAnywhere is the structural half of the never-send
// guarantee: not a test of behavior but of what can be named at all. A send
// added to the Mailbox seam or the provider fails here before it can be
// reached from a prompt.
func TestNoSendVerbExistsAnywhere(t *testing.T) {
	forbidden := []string{"Send", "Reply", "Forward", "Delete", "Trash"}

	surfaces := map[string]reflect.Type{
		"Mailbox":  reflect.TypeOf((*Mailbox)(nil)).Elem(),
		"provider": reflect.TypeOf(&provider{}),
	}
	for name, typ := range surfaces {
		for i := range typ.NumMethod() {
			method := typ.Method(i).Name
			for _, frag := range forbidden {
				if strings.Contains(method, frag) {
					t.Errorf("%s.%s matches forbidden verb %q — this package has no outbound mail path", name, method, frag)
				}
			}
		}
	}

	for _, tool := range Tools(fixture()) {
		for _, frag := range forbidden {
			if strings.Contains(strings.ToLower(tool.Name()), strings.ToLower(frag)) {
				t.Errorf("tool %s matches forbidden verb %q", tool.Name(), frag)
			}
		}
	}
}

func TestToolsRouteThroughTheMailbox(t *testing.T) {
	mb := fixture()
	byName := map[string]int{}
	tools := Tools(mb)
	for i, tool := range tools {
		byName[tool.Name()] = i
	}
	ctx := context.Background()

	run := func(name string, input map[string]any) {
		t.Helper()
		i, ok := byName[name]
		if !ok {
			t.Fatalf("no tool named %s", name)
		}
		out, err := tools[i].Execute(ctx, input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !out.Success {
			t.Fatalf("%s failed: %s", name, out.Error)
		}
	}

	run("list_messages", map[string]any{"account": "personal", "query": "is:unread"})
	run("get_message", map[string]any{"account": "personal", "id": "m-2"})
	run("apply_label", map[string]any{"account": "work", "message_id": "m-1", "label": "Lifedash/Noise"})
	run("remove_label", map[string]any{"account": "work", "message_id": "m-1", "label": "Lifedash/Noise"})
	run("archive_message", map[string]any{"account": "work", "message_id": "m-1"})

	want := []string{
		"list personal q=is:unread",
		"get personal m-2",
		"apply work m-1 Lifedash/Noise",
		"remove work m-1 Lifedash/Noise",
		"archive work m-1",
	}
	if !reflect.DeepEqual(mb.calls, want) {
		t.Fatalf("mailbox calls =\n  %v\nwant\n  %v", mb.calls, want)
	}
}

// TestARefusedWriteReportsAndDoesNothing covers the case the whole inversion
// exists for: the consumer's policy says no, and the tool layer neither
// performs the write nor hides the refusal from the model.
func TestARefusedWriteReportsAndDoesNothing(t *testing.T) {
	mb := fixture()
	mb.refuse = errors.New("permission denied: label \"Spam\" is outside the Lifedash/ namespace")

	var apply, archive int
	tools := Tools(mb)
	for i, tool := range tools {
		switch tool.Name() {
		case "apply_label":
			apply = i
		case "archive_message":
			archive = i
		}
	}
	ctx := context.Background()

	out, err := tools[apply].Execute(ctx, map[string]any{
		"account": "personal", "message_id": "m-1", "label": "Spam",
	})
	if err != nil {
		t.Fatalf("a refusal must be a failed tool result, not a transport error: %v", err)
	}
	if out.Success {
		t.Fatal("a refused apply_label reported success")
	}
	if !strings.Contains(out.Error, "outside the Lifedash/ namespace") {
		t.Fatalf("refusal %q does not carry the reason the mailbox gave", out.Error)
	}

	out, err = tools[archive].Execute(ctx, map[string]any{"account": "personal", "message_id": "m-1"})
	if err != nil || out.Success {
		t.Fatalf("a refused archive_message reported success (err=%v)", err)
	}

	if len(mb.calls) != 0 {
		t.Fatalf("a refused write still reached the mailbox: %v", mb.calls)
	}
}
