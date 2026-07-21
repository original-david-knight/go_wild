package gowild_agentic_loop

import (
	"encoding/base64"
	"encoding/json"

	"google.golang.org/genai"
)

// SerializedPart is a JSON-serializable representation of a genai.Part.
// It preserves thought signatures for database persistence.
type SerializedPart struct {
	Text                string                `json:"text,omitempty"`
	Thought             bool                  `json:"thought,omitempty"`
	ThoughtSignature    string                `json:"thought_signature,omitempty"` // base64 encoded
	FunctionCall        *FunctionCall         `json:"function_call,omitempty"`
	FunctionResponseID  string                `json:"function_response_id,omitempty"`
	FunctionResponse    map[string]any        `json:"function_response,omitempty"`
	FunctionName        string                `json:"function_name,omitempty"` // for function response
	InlineData          *SerializedBlob       `json:"inline_data,omitempty"`
	ExecutableCode      *SerializedCode       `json:"executable_code,omitempty"`
	CodeExecutionResult *SerializedCodeResult `json:"code_execution_result,omitempty"`
}

// SerializedCode is a JSON-serializable representation of ExecutableCode.
type SerializedCode struct {
	Code     string `json:"code"`
	Language string `json:"language"`
}

// SerializedCodeResult is a JSON-serializable representation of CodeExecutionResult.
type SerializedCodeResult struct {
	Outcome string `json:"outcome"`
	Output  string `json:"output,omitempty"`
}

// SerializedBlob is a JSON-serializable representation of inline data.
type SerializedBlob struct {
	Data     string `json:"data"` // base64 encoded
	MIMEType string `json:"mime_type"`
}

// FunctionCall is a JSON-serializable function call.
type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// SerializedContent is a JSON-serializable representation of a genai.Content.
type SerializedContent struct {
	Role  string           `json:"role"`
	Parts []SerializedPart `json:"parts"`
}

// serializedMessage is a JSON-serializable representation of a Message.
type serializedMessage struct {
	Role    string            `json:"role"`
	Content SerializedContent `json:"content"`
}

// serializeMessage converts a Message to a JSON-serializable format.
func serializeMessage(msg Message) serializedMessage {
	serialized := serializedMessage{
		Role: string(msg.Role),
		Content: SerializedContent{
			Role:  string(msg.Content.Role),
			Parts: make([]SerializedPart, 0, len(msg.Content.Parts)),
		},
	}

	for _, part := range msg.Content.Parts {
		sp := serializePart(part)
		serialized.Content.Parts = append(serialized.Content.Parts, sp)
	}

	return serialized
}

func serializePart(part *genai.Part) SerializedPart {
	sp := SerializedPart{
		Text:    part.Text,
		Thought: part.Thought,
	}

	if len(part.ThoughtSignature) > 0 {
		sp.ThoughtSignature = base64.StdEncoding.EncodeToString(part.ThoughtSignature)
	}

	if part.FunctionCall != nil {
		sp.FunctionCall = &FunctionCall{
			ID:   part.FunctionCall.ID,
			Name: part.FunctionCall.Name,
			Args: part.FunctionCall.Args,
		}
	}

	if part.FunctionResponse != nil {
		sp.FunctionResponseID = part.FunctionResponse.ID
		sp.FunctionName = part.FunctionResponse.Name
		sp.FunctionResponse = part.FunctionResponse.Response
	}

	if part.InlineData != nil {
		sp.InlineData = &SerializedBlob{
			Data:     base64.StdEncoding.EncodeToString(part.InlineData.Data),
			MIMEType: part.InlineData.MIMEType,
		}
	}

	if part.ExecutableCode != nil {
		sp.ExecutableCode = &SerializedCode{
			Code:     part.ExecutableCode.Code,
			Language: string(part.ExecutableCode.Language),
		}
	}

	if part.CodeExecutionResult != nil {
		sp.CodeExecutionResult = &SerializedCodeResult{
			Outcome: string(part.CodeExecutionResult.Outcome),
			Output:  part.CodeExecutionResult.Output,
		}
	}

	return sp
}

// deserializeMessage converts a serializedMessage back to a Message.
// Empty parts are filtered out to avoid Gemini API errors.
func deserializeMessage(sm serializedMessage) (Message, error) {
	msg := Message{
		Role: MessageRole(sm.Role),
		Content: &genai.Content{
			Role:  sm.Content.Role,
			Parts: make([]*genai.Part, 0, len(sm.Content.Parts)),
		},
	}

	for _, sp := range sm.Content.Parts {
		part, err := deserializePart(sp)
		if err != nil {
			return msg, err
		}
		if !isEmptyPart(part) {
			msg.Content.Parts = append(msg.Content.Parts, part)
		}
	}

	return msg, nil
}

func deserializePart(sp SerializedPart) (*genai.Part, error) {
	part := &genai.Part{
		Text:    sp.Text,
		Thought: sp.Thought,
	}

	if sp.ThoughtSignature != "" {
		sig, err := base64.StdEncoding.DecodeString(sp.ThoughtSignature)
		if err != nil {
			return nil, err
		}
		part.ThoughtSignature = sig
	}

	if sp.FunctionCall != nil {
		part.FunctionCall = &genai.FunctionCall{
			ID:   sp.FunctionCall.ID,
			Name: sp.FunctionCall.Name,
			Args: sp.FunctionCall.Args,
		}
	}

	if sp.FunctionResponse != nil {
		part.FunctionResponse = &genai.FunctionResponse{
			ID:       sp.FunctionResponseID,
			Name:     sp.FunctionName,
			Response: sp.FunctionResponse,
		}
	}

	if sp.InlineData != nil {
		data, err := base64.StdEncoding.DecodeString(sp.InlineData.Data)
		if err != nil {
			return nil, err
		}
		part.InlineData = &genai.Blob{
			Data:     data,
			MIMEType: sp.InlineData.MIMEType,
		}
	}

	if sp.ExecutableCode != nil {
		part.ExecutableCode = &genai.ExecutableCode{
			Code:     sp.ExecutableCode.Code,
			Language: genai.Language(sp.ExecutableCode.Language),
		}
	}

	if sp.CodeExecutionResult != nil {
		part.CodeExecutionResult = &genai.CodeExecutionResult{
			Outcome: genai.Outcome(sp.CodeExecutionResult.Outcome),
			Output:  sp.CodeExecutionResult.Output,
		}
	}

	return part, nil
}

// isEmptyPart returns true if a deserialized Part has no data fields set.
// The Gemini API rejects Parts with no oneof data field initialized.
func isEmptyPart(part *genai.Part) bool {
	if part.Text != "" {
		return false
	}
	if part.Thought && len(part.ThoughtSignature) > 0 {
		return false
	}
	return part.FunctionCall == nil &&
		part.FunctionResponse == nil &&
		part.InlineData == nil &&
		part.ExecutableCode == nil &&
		part.CodeExecutionResult == nil &&
		part.FileData == nil
}

// SerializeContent converts a genai.Content to a SerializedContent.
func SerializeContent(c *genai.Content) SerializedContent {
	sc := SerializedContent{
		Role:  c.Role,
		Parts: make([]SerializedPart, 0, len(c.Parts)),
	}
	for _, part := range c.Parts {
		sc.Parts = append(sc.Parts, serializePart(part))
	}
	return sc
}

// DeserializeContent converts a SerializedContent back to a genai.Content.
// Empty parts (no data fields set) are filtered out to avoid Gemini API errors.
func DeserializeContent(sc SerializedContent) (*genai.Content, error) {
	c := &genai.Content{
		Role:  sc.Role,
		Parts: make([]*genai.Part, 0, len(sc.Parts)),
	}
	for _, sp := range sc.Parts {
		part, err := deserializePart(sp)
		if err != nil {
			return nil, err
		}
		if !isEmptyPart(part) {
			c.Parts = append(c.Parts, part)
		}
	}
	return c, nil
}

// DeserializeContents converts a slice of SerializedContent to []*genai.Content.
func DeserializeContents(serialized []SerializedContent) ([]*genai.Content, error) {
	contents := make([]*genai.Content, len(serialized))
	for i, sc := range serialized {
		c, err := DeserializeContent(sc)
		if err != nil {
			return nil, err
		}
		contents[i] = c
	}
	return contents, nil
}

// SerializeContents converts []*genai.Content to a slice of SerializedContent.
func SerializeContents(contents []*genai.Content) []SerializedContent {
	serialized := make([]SerializedContent, len(contents))
	for i, c := range contents {
		serialized[i] = SerializeContent(c)
	}
	return serialized
}

// SerializeTools converts []*genai.Tool to JSON bytes.
func SerializeTools(tools []*genai.Tool) (json.RawMessage, error) {
	return json.Marshal(tools)
}

// DeserializeTools converts JSON bytes back to []*genai.Tool.
func DeserializeTools(data json.RawMessage) ([]*genai.Tool, error) {
	var tools []*genai.Tool
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// SerializeHistory converts a slice of Messages to JSON.
func SerializeHistory(history []Message) ([]byte, error) {
	serialized := make([]serializedMessage, len(history))
	for i, msg := range history {
		serialized[i] = serializeMessage(msg)
	}
	return json.Marshal(serialized)
}

// DeserializeHistory converts JSON back to a slice of Messages.
func DeserializeHistory(data []byte) ([]Message, error) {
	var serialized []serializedMessage
	if err := json.Unmarshal(data, &serialized); err != nil {
		return nil, err
	}

	messages := make([]Message, len(serialized))
	for i, sm := range serialized {
		msg, err := deserializeMessage(sm)
		if err != nil {
			return nil, err
		}
		messages[i] = msg
	}
	return messages, nil
}
