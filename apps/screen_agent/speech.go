package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/genai"
)

type SpeechAudio struct {
	Data         []byte
	MIMEType     string
	FinishReason string
}

type SpeechGenerator interface {
	GenerateSpeech(ctx context.Context, model, voice, languageCode, text string) (SpeechAudio, error)
}

type GeminiSpeechGenerator struct {
	Client *genai.Client
}

func NewGeminiSpeechGenerator(ctx context.Context, cfg Config, getenv func(string) string) (*GeminiSpeechGenerator, error) {
	apiKey, keyName := geminiAPIKey(getenv)
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY is required for Gemini TTS")
	}
	clientConfig := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}
	if baseURL := strings.TrimSpace(cfg.AgentBaseURL); baseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{BaseURL: baseURL}
	}
	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("create Gemini TTS client using %s: %w", keyName, err)
	}
	return &GeminiSpeechGenerator{Client: client}, nil
}

func (g *GeminiSpeechGenerator) GenerateSpeech(ctx context.Context, model, voice, languageCode, text string) (SpeechAudio, error) {
	config := &genai.GenerateContentConfig{
		ResponseModalities: []string{"AUDIO"},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: strings.TrimSpace(languageCode),
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: strings.TrimSpace(voice),
				},
			},
		},
	}
	prompt := "Say in a clear, calm screen-assistant voice: " + text
	resp, err := g.Client.Models.GenerateContent(ctx, strings.TrimSpace(model), genai.Text(prompt), config)
	if err != nil {
		return SpeechAudio{}, err
	}
	audio, err := extractSpeechAudio(resp)
	if err != nil {
		return SpeechAudio{}, err
	}
	return audio, nil
}

type GeminiSpeaker struct {
	Config    Config
	Runner    CommandRunner
	Getenv    func(string) string
	Logger    DebugLogger
	Generator SpeechGenerator
}

type PreparedSpeech struct {
	Path    string
	Cleanup func()
}

func NewGeminiSpeaker(ctx context.Context, cfg Config, runner CommandRunner, getenv func(string) string, logger DebugLogger) (*GeminiSpeaker, error) {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	generator, err := NewGeminiSpeechGenerator(ctx, cfg, getenv)
	if err != nil {
		return nil, err
	}
	return &GeminiSpeaker{
		Config:    cfg,
		Runner:    runner,
		Getenv:    getenv,
		Logger:    logger,
		Generator: generator,
	}, nil
}

func (s *GeminiSpeaker) Speak(ctx context.Context, text string) error {
	prepared, err := s.Prepare(ctx, text)
	if err != nil {
		return err
	}
	if prepared.Path == "" {
		return nil
	}
	if prepared.Cleanup != nil {
		defer prepared.Cleanup()
	}
	return s.Play(ctx, prepared.Path)
}

func (s *GeminiSpeaker) Prepare(ctx context.Context, text string) (PreparedSpeech, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return PreparedSpeech{}, nil
	}

	generateCtx, cancelGenerate := context.WithTimeout(ctx, s.Config.TTSTimeout)
	audio, err := s.Generator.GenerateSpeech(generateCtx, s.Config.TTSModel, s.Config.TTSVoice, s.Config.TTSLanguageCode, text)
	cancelGenerate()
	if err != nil {
		return PreparedSpeech{}, err
	}
	s.Logger.Printf("tts response: finish_reason=%q mime=%q bytes=%d", audio.FinishReason, audio.MIMEType, len(audio.Data))

	audioFile, err := playableAudio(audio)
	if err != nil {
		return PreparedSpeech{}, err
	}
	path, cleanup, err := s.writeTempAudio(audioFile)
	if err != nil {
		return PreparedSpeech{}, err
	}
	return PreparedSpeech{Path: path, Cleanup: cleanup}, nil
}

func (s *GeminiSpeaker) Play(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	player, ok, err := ResolveAudioPlayerCommand(s.Config.TTSPlayerCommand, s.Runner)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no usable audio player command found for Gemini TTS")
	}

	args := commandArgsWithFile(player.Args, path)
	playbackCtx, cancelPlayback := context.WithTimeout(ctx, s.Config.TTSPlaybackTimeout)
	defer cancelPlayback()
	_, err = s.Runner.Run(playbackCtx, player.Name, args...)
	if err != nil && playbackCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("TTS playback exceeded tts_playback_timeout %s: %w", s.Config.TTSPlaybackTimeout, err)
	}
	return err
}

func (s *GeminiSpeaker) writeTempAudio(audio []byte) (string, func(), error) {
	dir := RuntimeDir(s.Getenv)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create runtime dir for TTS audio: %w", err)
	}
	f, err := os.CreateTemp(dir, "tts-*.wav")
	if err != nil {
		return "", nil, fmt.Errorf("create TTS audio file: %w", err)
	}
	path := f.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if _, err := f.Write(audio); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write TTS audio file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close TTS audio file: %w", err)
	}
	return path, cleanup, nil
}

func ResolveAudioPlayerCommand(raw string, runner CommandRunner) (CommandSpec, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "off") {
		return CommandSpec{}, false, nil
	}
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if !strings.EqualFold(raw, "auto") {
		spec, err := ParseCommandSpec(raw)
		if err != nil {
			return CommandSpec{}, false, fmt.Errorf("tts_player_command: %w", err)
		}
		if _, err := runner.LookPath(spec.Name); err != nil {
			return CommandSpec{}, false, fmt.Errorf("TTS audio player %q not found", spec.Name)
		}
		return spec, true, nil
	}
	for _, spec := range autoAudioPlayerSpecs() {
		if _, err := runner.LookPath(spec.Name); err == nil {
			return spec, true, nil
		}
	}
	return CommandSpec{}, false, nil
}

func commandArgsWithFile(args []string, path string) []string {
	out := append([]string{}, args...)
	replaced := false
	for i, arg := range out {
		if strings.Contains(arg, "{file}") {
			out[i] = strings.ReplaceAll(arg, "{file}", path)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, path)
	}
	return out
}

func extractSpeechAudio(resp *genai.GenerateContentResponse) (SpeechAudio, error) {
	if resp == nil {
		return SpeechAudio{}, fmt.Errorf("Gemini TTS returned nil response")
	}
	finishReason := ""
	for _, candidate := range resp.Candidates {
		if candidate == nil {
			continue
		}
		if finishReason == "" && candidate.FinishReason != "" {
			finishReason = string(candidate.FinishReason)
		}
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part == nil || part.InlineData == nil || len(part.InlineData.Data) == 0 {
				continue
			}
			return SpeechAudio{
				Data:         part.InlineData.Data,
				MIMEType:     part.InlineData.MIMEType,
				FinishReason: finishReason,
			}, nil
		}
	}
	return SpeechAudio{}, fmt.Errorf("Gemini TTS returned no inline audio; finish_reason=%q", finishReason)
}

func playableAudio(audio SpeechAudio) ([]byte, error) {
	if len(audio.Data) == 0 {
		return nil, fmt.Errorf("Gemini TTS returned empty audio")
	}
	mimeType := strings.ToLower(strings.TrimSpace(audio.MIMEType))
	if bytes.HasPrefix(audio.Data, []byte("RIFF")) || strings.Contains(mimeType, "wav") {
		return audio.Data, nil
	}
	if len(audio.Data)%2 != 0 {
		return nil, fmt.Errorf("Gemini TTS returned odd-length PCM audio")
	}
	return pcm16MonoWAV(audio.Data, sampleRateFromMIME(mimeType))
}

func sampleRateFromMIME(mimeType string) int {
	mimeType = strings.ToLower(mimeType)
	for _, match := range sampleRateRE.FindAllStringSubmatch(mimeType, -1) {
		if len(match) < 2 {
			continue
		}
		n, err := strconv.Atoi(match[1])
		if err == nil && n > 0 {
			return n
		}
	}
	return 24000
}

func pcm16MonoWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if sampleRate <= 0 {
		return nil, fmt.Errorf("sample rate must be positive")
	}
	const (
		channels      = 1
		bitsPerSample = 16
		audioFormat   = 1
	)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	dataLen := uint32(len(pcm))
	riffLen := uint32(36 + len(pcm))

	var buf bytes.Buffer
	write := func(v any) error {
		return binary.Write(&buf, binary.LittleEndian, v)
	}
	if _, err := io.WriteString(&buf, "RIFF"); err != nil {
		return nil, err
	}
	if err := write(riffLen); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(&buf, "WAVEfmt "); err != nil {
		return nil, err
	}
	if err := write(uint32(16)); err != nil {
		return nil, err
	}
	if err := write(uint16(audioFormat)); err != nil {
		return nil, err
	}
	if err := write(uint16(channels)); err != nil {
		return nil, err
	}
	if err := write(uint32(sampleRate)); err != nil {
		return nil, err
	}
	if err := write(uint32(byteRate)); err != nil {
		return nil, err
	}
	if err := write(uint16(blockAlign)); err != nil {
		return nil, err
	}
	if err := write(uint16(bitsPerSample)); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(&buf, "data"); err != nil {
		return nil, err
	}
	if err := write(dataLen); err != nil {
		return nil, err
	}
	if _, err := buf.Write(pcm); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func geminiAPIKey(getenv func(string) string) (string, string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if key := strings.TrimSpace(getenv("GOOGLE_API_KEY")); key != "" {
		return key, "GOOGLE_API_KEY"
	}
	if key := strings.TrimSpace(getenv("GEMINI_API_KEY")); key != "" {
		return key, "GEMINI_API_KEY"
	}
	return "", ""
}

func SpokenText(result AnalyzeResult, cfg Config) (string, bool, string) {
	return SpokenTextForIntent(result, cfg, AssistIntentAuto)
}

func SpokenTextForIntent(result AnalyzeResult, cfg Config, intent AssistIntent) (string, bool, string) {
	if !result.QuestionFound {
		return "", false, "no answerable screen content found"
	}
	if result.Confidence == ConfidenceLow && !cfg.SpeakWhenUncertain && intent == AssistIntentAuto {
		return "", false, "low confidence suppressed"
	}
	text := sanitizeSpeech(result.SpokenAnswer, cfg.MaxSpokenChars)
	if text == "" {
		return "", false, "empty spoken answer"
	}
	return text, true, ""
}

func sanitizeSpeech(text string, maxChars int) string {
	text = stripCodeFence(text)
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "{") || strings.HasSuffix(trimmed, "}") {
			continue
		}
		if strings.Count(trimmed, "|") >= 2 {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	text = strings.Join(filtered, " ")
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "#", "")
	text = markdownEmphasisRE.ReplaceAllString(text, "$1")
	text = whitespaceRE.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	if maxChars > 0 && utf8.RuneCountInString(text) > maxChars {
		text = truncateAtSentence(text, maxChars)
	}
	return strings.TrimSpace(text)
}

func truncateAtSentence(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	cut := string(runes[:maxChars])
	best := -1
	for _, sep := range []string{". ", "? ", "! ", "; ", ": "} {
		if i := strings.LastIndex(cut, sep); i > best {
			best = i + 1
		}
	}
	if best >= 0 && utf8.RuneCountInString(cut[:best]) >= maxChars/3 {
		return strings.TrimSpace(cut[:best])
	}
	cut = strings.TrimSpace(cut)
	if i := strings.LastIndex(cut, " "); i >= 0 && utf8.RuneCountInString(cut[:i]) >= maxChars/3 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "..."
}

var (
	whitespaceRE       = regexp.MustCompile(`\s+`)
	markdownEmphasisRE = regexp.MustCompile(`\*\*?([^*]+)\*\*?`)
	sampleRateRE       = regexp.MustCompile(`(?:rate|sample_rate)=([0-9]+)`)
)
