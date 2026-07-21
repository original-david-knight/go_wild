package main

import (
	"strings"
	"testing"
)

func TestParseAssistIntentAliases(t *testing.T) {
	tests := map[string]AssistIntent{
		"":                AssistIntentAuto,
		"auto":            AssistIntentAuto,
		"fact-check":      AssistIntentFactCheck,
		"fact_check":      AssistIntentFactCheck,
		"summary":         AssistIntentEnglishSummary,
		"summarize":       AssistIntentEnglishSummary,
		"english-summary": AssistIntentEnglishSummary,
	}
	for input, want := range tests {
		got, err := ParseAssistIntent(input)
		if err != nil {
			t.Fatalf("ParseAssistIntent(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseAssistIntent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseAssistIntentRejectsUnknown(t *testing.T) {
	if _, err := ParseAssistIntent("invent"); err == nil {
		t.Fatalf("expected unknown intent error")
	}
}

func TestFactCheckInstructionRequiresGroundedEnglishVerdict(t *testing.T) {
	instruction := AssistIntentFactCheck.systemInstruction()
	for _, want := range []string{"Google Search grounding", "externally verifiable", "Respond in concise English", "Could not verify:"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("fact-check instruction missing %q: %s", want, instruction)
		}
	}
}

func TestEnglishSummaryInstructionTranslatesWithoutSolving(t *testing.T) {
	instruction := AssistIntentEnglishSummary.systemInstruction()
	for _, want := range []string{"regardless of the language", "Translate the meaning into English", "Do not solve embedded exercises"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("English-summary instruction missing %q: %s", want, instruction)
		}
	}
}
