package gowild_agentic_loop

import (
	"encoding/base64"
	"testing"

	"google.golang.org/genai"
)

func TestSerializeMessage_Text(t *testing.T) {
	msg := NewUserMessage("Hello, world!")
	serialized := serializeMessage(msg)

	if serialized.Role != "user" {
		t.Errorf("expected role 'user', got %s", serialized.Role)
	}
	if len(serialized.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(serialized.Content.Parts))
	}
	if serialized.Content.Parts[0].Text != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %s", serialized.Content.Parts[0].Text)
	}
}

func TestSerializeMessage_WithThoughtSignature(t *testing.T) {
	signature := []byte{0x01, 0x02, 0x03, 0x04}
	msg := Message{
		Role: RoleModel,
		Content: &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{
					Text:             "Thinking...",
					Thought:          true,
					ThoughtSignature: signature,
				},
				{
					Text: "Final answer",
				},
			},
		},
	}

	serialized := serializeMessage(msg)

	if len(serialized.Content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(serialized.Content.Parts))
	}

	// Check thought part
	thoughtPart := serialized.Content.Parts[0]
	if !thoughtPart.Thought {
		t.Error("expected thought to be true")
	}
	if thoughtPart.ThoughtSignature != base64.StdEncoding.EncodeToString(signature) {
		t.Error("thought signature not correctly encoded")
	}

	// Check regular part
	regularPart := serialized.Content.Parts[1]
	if regularPart.Thought {
		t.Error("expected thought to be false for regular part")
	}
	if regularPart.ThoughtSignature != "" {
		t.Error("expected no thought signature for regular part")
	}
}

func TestSerializeMessage_FunctionCall(t *testing.T) {
	msg := Message{
		Role: RoleModel,
		Content: &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "call_123",
						Name: "get_weather",
						Args: map[string]any{"location": "SF"},
					},
				},
			},
		},
	}

	serialized := serializeMessage(msg)

	if len(serialized.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(serialized.Content.Parts))
	}

	fc := serialized.Content.Parts[0].FunctionCall
	if fc == nil {
		t.Fatal("expected function call")
	}
	if fc.ID != "call_123" {
		t.Errorf("expected ID 'call_123', got %s", fc.ID)
	}
	if fc.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %s", fc.Name)
	}
	if fc.Args["location"] != "SF" {
		t.Errorf("expected location 'SF', got %v", fc.Args["location"])
	}
}

func TestDeserializeMessage_Text(t *testing.T) {
	serialized := serializedMessage{
		Role: "user",
		Content: SerializedContent{
			Role: "user",
			Parts: []SerializedPart{
				{Text: "Hello!"},
			},
		},
	}

	msg, err := deserializeMessage(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Role != RoleUser {
		t.Errorf("expected role 'user', got %s", msg.Role)
	}
	if len(msg.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Content.Parts))
	}
	if msg.Content.Parts[0].Text != "Hello!" {
		t.Errorf("expected text 'Hello!', got %s", msg.Content.Parts[0].Text)
	}
}

func TestDeserializeMessage_WithThoughtSignature(t *testing.T) {
	signature := []byte{0x01, 0x02, 0x03, 0x04}
	encodedSig := base64.StdEncoding.EncodeToString(signature)

	serialized := serializedMessage{
		Role: "model",
		Content: SerializedContent{
			Role: "model",
			Parts: []SerializedPart{
				{
					Text:             "Thinking...",
					Thought:          true,
					ThoughtSignature: encodedSig,
				},
			},
		},
	}

	msg, err := deserializeMessage(serialized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	part := msg.Content.Parts[0]
	if !part.Thought {
		t.Error("expected thought to be true")
	}
	if string(part.ThoughtSignature) != string(signature) {
		t.Error("thought signature not correctly decoded")
	}
}

func TestSerializeDeserializeHistory_RoundTrip(t *testing.T) {
	signature := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	history := []Message{
		NewUserMessage("What is 2+2?"),
		{
			Role: RoleModel,
			Content: &genai.Content{
				Role: genai.RoleModel,
				Parts: []*genai.Part{
					{
						Text:             "Let me think...",
						Thought:          true,
						ThoughtSignature: signature,
					},
					{
						FunctionCall: &genai.FunctionCall{
							ID:   "calc_1",
							Name: "calculate",
							Args: map[string]any{"expr": "2+2"},
						},
					},
				},
			},
		},
		NewToolResultMessage("calculate", map[string]any{"result": 4}),
		NewModelTextMessage("The answer is 4."),
	}

	// Serialize
	data, err := SerializeHistory(history)
	if err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	// Deserialize
	restored, err := DeserializeHistory(data)
	if err != nil {
		t.Fatalf("deserialize error: %v", err)
	}

	// Verify
	if len(restored) != len(history) {
		t.Fatalf("expected %d messages, got %d", len(history), len(restored))
	}

	// Check user message
	if restored[0].Role != RoleUser {
		t.Error("first message should be user")
	}

	// Check thought signature preserved
	modelMsg := restored[1]
	if len(modelMsg.Content.Parts) != 2 {
		t.Fatalf("expected 2 parts in model message, got %d", len(modelMsg.Content.Parts))
	}
	if !modelMsg.Content.Parts[0].Thought {
		t.Error("thought flag not preserved")
	}
	if string(modelMsg.Content.Parts[0].ThoughtSignature) != string(signature) {
		t.Error("thought signature not preserved")
	}

	// Check function call preserved
	fc := modelMsg.Content.Parts[1].FunctionCall
	if fc == nil {
		t.Fatal("function call not preserved")
	}
	if fc.Name != "calculate" {
		t.Errorf("expected function name 'calculate', got %s", fc.Name)
	}
}

func TestDeserializeContent_FiltersEmptyParts(t *testing.T) {
	sc := SerializedContent{
		Role: "model",
		Parts: []SerializedPart{
			{Text: "Hello"},
			{}, // empty part — should be filtered out
			{Text: "World"},
		},
	}

	content, err := DeserializeContent(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts (empty filtered), got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "Hello" {
		t.Errorf("expected 'Hello', got %q", content.Parts[0].Text)
	}
	if content.Parts[1].Text != "World" {
		t.Errorf("expected 'World', got %q", content.Parts[1].Text)
	}
}

func TestSerializePart_ExecutableCode(t *testing.T) {
	part := &genai.Part{
		ExecutableCode: &genai.ExecutableCode{
			Code:     "print('hello')",
			Language: "PYTHON",
		},
	}

	sp := serializePart(part)
	if sp.ExecutableCode == nil {
		t.Fatal("expected executable code")
	}
	if sp.ExecutableCode.Code != "print('hello')" {
		t.Errorf("expected code 'print('hello')', got %s", sp.ExecutableCode.Code)
	}
	if sp.ExecutableCode.Language != "PYTHON" {
		t.Errorf("expected language 'PYTHON', got %s", sp.ExecutableCode.Language)
	}

	// Round-trip
	restored, err := deserializePart(sp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restored.ExecutableCode == nil {
		t.Fatal("executable code not preserved")
	}
	if restored.ExecutableCode.Code != "print('hello')" {
		t.Error("code not preserved")
	}
}

func TestSerializePart_CodeExecutionResult(t *testing.T) {
	part := &genai.Part{
		CodeExecutionResult: &genai.CodeExecutionResult{
			Outcome: "OUTCOME_OK",
			Output:  "hello\n",
		},
	}

	sp := serializePart(part)
	if sp.CodeExecutionResult == nil {
		t.Fatal("expected code execution result")
	}

	// Round-trip
	restored, err := deserializePart(sp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restored.CodeExecutionResult == nil {
		t.Fatal("code execution result not preserved")
	}
	if string(restored.CodeExecutionResult.Outcome) != "OUTCOME_OK" {
		t.Error("outcome not preserved")
	}
	if restored.CodeExecutionResult.Output != "hello\n" {
		t.Error("output not preserved")
	}
}

func TestDeserializeMessage_InvalidBase64(t *testing.T) {
	serialized := serializedMessage{
		Role: "model",
		Content: SerializedContent{
			Role: "model",
			Parts: []SerializedPart{
				{
					ThoughtSignature: "not-valid-base64!!!",
				},
			},
		},
	}

	_, err := deserializeMessage(serialized)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestSerializeTools_RoundTrip(t *testing.T) {
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "get_weather",
					Description: "Get the weather for a location",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"location": {
								Type:        genai.TypeString,
								Description: "The city name",
							},
						},
						Required: []string{"location"},
					},
				},
			},
		},
	}

	data, err := SerializeTools(tools)
	if err != nil {
		t.Fatalf("SerializeTools error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("SerializeTools returned empty data")
	}

	restored, err := DeserializeTools(data)
	if err != nil {
		t.Fatalf("DeserializeTools error: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(restored))
	}
	if len(restored[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(restored[0].FunctionDeclarations))
	}
	fd := restored[0].FunctionDeclarations[0]
	if fd.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", fd.Name)
	}
	if fd.Description != "Get the weather for a location" {
		t.Errorf("expected description to match, got %q", fd.Description)
	}
	if fd.Parameters == nil || len(fd.Parameters.Required) != 1 || fd.Parameters.Required[0] != "location" {
		t.Errorf("expected required field 'location', got %v", fd.Parameters)
	}
}

func TestSerializeTools_Empty(t *testing.T) {
	data, err := SerializeTools([]*genai.Tool{})
	if err != nil {
		t.Fatalf("SerializeTools error: %v", err)
	}

	restored, err := DeserializeTools(data)
	if err != nil {
		t.Fatalf("DeserializeTools error: %v", err)
	}
	if len(restored) != 0 {
		t.Errorf("expected 0 tools, got %d", len(restored))
	}
}

func TestDeserializeTools_InvalidJSON(t *testing.T) {
	_, err := DeserializeTools([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSerializeTools_MultipleDeclarations(t *testing.T) {
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "tool_a", Description: "First tool"},
				{Name: "tool_b", Description: "Second tool"},
			},
		},
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "tool_c", Description: "Third tool"},
			},
		},
	}

	data, err := SerializeTools(tools)
	if err != nil {
		t.Fatalf("SerializeTools error: %v", err)
	}

	restored, err := DeserializeTools(data)
	if err != nil {
		t.Fatalf("DeserializeTools error: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(restored))
	}
	if len(restored[0].FunctionDeclarations) != 2 {
		t.Errorf("expected 2 declarations in first tool, got %d", len(restored[0].FunctionDeclarations))
	}
	if len(restored[1].FunctionDeclarations) != 1 {
		t.Errorf("expected 1 declaration in second tool, got %d", len(restored[1].FunctionDeclarations))
	}
}

func TestSerializePart_InlineData_RoundTrip(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	part := &genai.Part{
		InlineData: &genai.Blob{
			Data:     imageData,
			MIMEType: "image/png",
		},
	}

	sp := serializePart(part)
	if sp.InlineData == nil {
		t.Fatal("expected inline data")
	}
	if sp.InlineData.MIMEType != "image/png" {
		t.Errorf("expected MIME type 'image/png', got %q", sp.InlineData.MIMEType)
	}

	// Verify base64 encoding
	expectedB64 := base64.StdEncoding.EncodeToString(imageData)
	if sp.InlineData.Data != expectedB64 {
		t.Errorf("expected base64 data %q, got %q", expectedB64, sp.InlineData.Data)
	}

	// Round-trip
	restored, err := deserializePart(sp)
	if err != nil {
		t.Fatalf("deserializePart error: %v", err)
	}
	if restored.InlineData == nil {
		t.Fatal("inline data not preserved")
	}
	if string(restored.InlineData.Data) != string(imageData) {
		t.Error("image data not preserved in round-trip")
	}
	if restored.InlineData.MIMEType != "image/png" {
		t.Errorf("MIME type not preserved: got %q", restored.InlineData.MIMEType)
	}
}

func TestDeserializePart_InvalidBase64InlineData(t *testing.T) {
	sp := SerializedPart{
		InlineData: &SerializedBlob{
			Data:     "not-valid-base64!!!",
			MIMEType: "image/png",
		},
	}

	_, err := deserializePart(sp)
	if err == nil {
		t.Error("expected error for invalid base64 inline data")
	}
}

func TestSerializeMessage_FunctionResponse(t *testing.T) {
	msg := Message{
		Role: RoleTool,
		Content: &genai.Content{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						ID:       "call_weather",
						Name:     "get_weather",
						Response: map[string]any{"temp": 72, "condition": "sunny"},
					},
				},
			},
		},
	}

	serialized := serializeMessage(msg)
	if len(serialized.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(serialized.Content.Parts))
	}

	sp := serialized.Content.Parts[0]
	if sp.FunctionName != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %q", sp.FunctionName)
	}
	if sp.FunctionResponse == nil {
		t.Fatal("expected function response")
	}
	if sp.FunctionResponseID != "call_weather" {
		t.Fatalf("expected function response ID call_weather, got %q", sp.FunctionResponseID)
	}
	if sp.FunctionResponse["temp"] != float64(72) && sp.FunctionResponse["temp"] != 72 {
		t.Errorf("expected temp 72, got %v", sp.FunctionResponse["temp"])
	}

	// Round-trip
	restored, err := deserializeMessage(serialized)
	if err != nil {
		t.Fatalf("deserializeMessage error: %v", err)
	}
	fr := restored.Content.Parts[0].FunctionResponse
	if fr == nil {
		t.Fatal("function response not preserved")
	}
	if fr.Name != "get_weather" {
		t.Errorf("function response name not preserved: got %q", fr.Name)
	}
	if fr.ID != "call_weather" {
		t.Errorf("function response ID not preserved: got %q", fr.ID)
	}
}

func TestSerializeContents_RoundTrip(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("hello", genai.RoleUser),
		genai.NewContentFromText("world", genai.RoleModel),
	}

	serialized := SerializeContents(contents)
	if len(serialized) != 2 {
		t.Fatalf("expected 2 serialized contents, got %d", len(serialized))
	}

	restored, err := DeserializeContents(serialized)
	if err != nil {
		t.Fatalf("DeserializeContents error: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 restored contents, got %d", len(restored))
	}
	if restored[0].Parts[0].Text != "hello" {
		t.Errorf("expected 'hello', got %q", restored[0].Parts[0].Text)
	}
	if restored[1].Parts[0].Text != "world" {
		t.Errorf("expected 'world', got %q", restored[1].Parts[0].Text)
	}
}
