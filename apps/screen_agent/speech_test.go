package main

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/genai"
)

type fakeSpeechGenerator struct {
	audio SpeechAudio
}

func (g fakeSpeechGenerator) GenerateSpeech(_ context.Context, _, _, _, _ string) (SpeechAudio, error) {
	return g.audio, nil
}

func TestSpokenTextSuppressesLowConfidenceByDefault(t *testing.T) {
	cfg := DefaultConfig()
	text, ok, reason := SpokenText(AnalyzeResult{
		QuestionFound: true,
		QuestionCount: 1,
		Confidence:    ConfidenceLow,
		SpokenAnswer:  "Maybe B.",
	}, cfg)
	if ok || text != "" || reason == "" {
		t.Fatalf("expected suppression, got ok=%v text=%q reason=%q", ok, text, reason)
	}
}

func TestSpokenTextSpeaksExplicitFactCheckUncertainty(t *testing.T) {
	cfg := DefaultConfig()
	text, ok, reason := SpokenTextForIntent(AnalyzeResult{
		QuestionFound: true,
		QuestionCount: 1,
		Confidence:    ConfidenceLow,
		SpokenAnswer:  "Could not verify: no grounded sources were returned.",
	}, cfg, AssistIntentFactCheck)
	if !ok || text == "" || reason != "" {
		t.Fatalf("expected explicit uncertainty to be spoken, got ok=%v text=%q reason=%q", ok, text, reason)
	}
}

func TestSanitizeSpeechDropsTablesAndTruncates(t *testing.T) {
	got := sanitizeSpeech("The answer is B.\n| a | b |\n| - | - |\nThis is extra text that should be removed after the sentence.", 20)
	if got != "The answer is B." {
		t.Fatalf("sanitizeSpeech = %q", got)
	}
}

func TestSanitizeSpeechTruncatesUnicodeAtRuneBoundary(t *testing.T) {
	got := sanitizeSpeech("你好世界和平", 5)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeSpeech returned invalid UTF-8: %q", got)
	}
	if got != "你好世界和..." {
		t.Fatalf("sanitizeSpeech = %q, want rune-safe truncation", got)
	}
}

func TestExtractSpeechAudioFindsInlineData(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{Parts: []*genai.Part{{
				InlineData: &genai.Blob{
					Data:     []byte{0x01, 0x02},
					MIMEType: "audio/L16;codec=pcm;rate=24000",
				},
			}}},
		}},
	}
	audio, err := extractSpeechAudio(resp)
	if err != nil {
		t.Fatalf("extractSpeechAudio returned error: %v", err)
	}
	if audio.FinishReason != "STOP" || audio.MIMEType != "audio/L16;codec=pcm;rate=24000" || len(audio.Data) != 2 {
		t.Fatalf("unexpected audio: %#v", audio)
	}
}

func TestPlayableAudioWrapsPCMAsWAV(t *testing.T) {
	wav, err := playableAudio(SpeechAudio{
		Data:     []byte{0x00, 0x00, 0x01, 0x00},
		MIMEType: "audio/L16;codec=pcm;rate=16000",
	})
	if err != nil {
		t.Fatalf("playableAudio returned error: %v", err)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("missing WAV header: %q %q", wav[:4], wav[8:12])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != 4 {
		t.Fatalf("data length = %d, want 4", got)
	}
}

func TestResolveAudioPlayerCommandAutoUsesFirstCandidate(t *testing.T) {
	spec, ok, err := ResolveAudioPlayerCommand("auto", &fakeRunner{})
	if err != nil {
		t.Fatalf("ResolveAudioPlayerCommand returned error: %v", err)
	}
	want := autoAudioPlayerSpecs()[0].Name
	if !ok || spec.Name != want {
		t.Fatalf("spec = %#v ok=%v, want %s", spec, ok, want)
	}
}

func TestCommandArgsWithFileSupportsPlaceholder(t *testing.T) {
	got := commandArgsWithFile([]string{"--input={file}", "--quiet"}, "/tmp/answer.wav")
	want := []string{"--input=/tmp/answer.wav", "--quiet"}
	if len(got) != len(want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
}

func TestGeminiSpeakerPrepareThenPlay(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	cfg := DefaultConfig()
	cfg.TTSPlayerCommand = "pw-play --volume 0.8"
	cfg.TTSPlaybackTimeout = 30 * time.Second
	speaker := &GeminiSpeaker{
		Config: cfg,
		Runner: runner,
		Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir}),
		Generator: fakeSpeechGenerator{audio: SpeechAudio{
			Data:     []byte{0x00, 0x00, 0x01, 0x00},
			MIMEType: "audio/L16;codec=pcm;rate=24000",
		}},
	}

	prepared, err := speaker.Prepare(context.Background(), "The answer is B.")
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	defer prepared.Cleanup()
	if prepared.Path == "" {
		t.Fatalf("Prepare returned empty path")
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("read prepared audio: %v", err)
	}
	if string(data[:4]) != "RIFF" {
		t.Fatalf("prepared audio did not get WAV header")
	}
	if err := speaker.Play(context.Background(), prepared.Path); err != nil {
		t.Fatalf("Play returned error: %v", err)
	}
	got := runner.commands[len(runner.commands)-1]
	want := []string{"pw-play", "--volume", "0.8", prepared.Path}
	if len(got) != len(want) {
		t.Fatalf("player command = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("player command = %#v, want %#v", got, want)
		}
	}
}
