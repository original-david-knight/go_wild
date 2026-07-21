# screen-agent

Local focused-window screen assistant for Hyprland (Linux) and Windows.

The app runs as a quiet listener. When the configured key combo fires, it captures the currently focused window, sends that screenshot image to a configured vision-capable LLM, and speaks concise help when the screen contains an answerable question, error, dialog, document, UI state, or other useful context. It does not edit Hyprland config, automate websites, fill forms, or include proctoring bypass behavior.

On Linux the capture backend is `grim` + `hyprctl`. On Windows the capture backend is native GDI (`screenshot_backend = "windows"`, the default there) — no external tools are needed.

## Build

```sh
cd ~/workspace/go_wild/apps/screen_agent
go build -o screen-agent .
```

Put the built binary on `PATH` if you want to call it from a Hyprland binding.

Example user install:

```sh
install -Dm755 screen-agent ~/.local/bin/screen-agent
install -Dm644 screen-agent.service.example ~/.config/systemd/user/screen-agent.service
systemctl --user daemon-reload
systemctl --user enable --now screen-agent.service
```

## Commands

```sh
screen-agent check
screen-agent
screen-agent daemon
screen-agent assist
screen-agent fact-check
screen-agent summarize
screen-agent capture --output /tmp/screen-agent-test.png
screen-agent analyze --image /tmp/screen-agent-test.png
screen-agent analyze --image /tmp/screen-agent-test.png --intent english-summary
screen-agent speak "The answer is B."
```

Running with no command starts the listener. `daemon` is the same explicit mode. The listener writes its PID to `$XDG_RUNTIME_DIR/screen-agent/screen-agent.pid`, listens for private commands on `$XDG_RUNTIME_DIR/screen-agent/screen-agent.sock`, and continues to handle `SIGUSR1` as the default key-combo event on Linux. When a request arrives, it captures the focused window, starts an occasional thinking tone while the LLM request is in flight, stops the tone, and then speaks the sanitized response if one should be spoken.

`assist` sends the default auto-assist intent to the running daemon over the control socket — the same behavior as the key-combo event, usable from any hotkey tool or script.

`fact-check` finds the main externally verifiable claim visible on screen, uses Gemini Google Search grounding, and speaks a short English verdict beginning with `Supported`, `Contradicted`, `Misleading`, or `Could not verify`. It requires `agent_provider = "gemini"`. A verdict is withheld when the model response contains no usable web grounding. Manual `analyze --intent fact-check` output includes up to five grounding source URLs in `sources`.

Fact-check uses one grounded-search request under the configured `agent_timeout`. If grounding does not substantively support the proposed verdict, the command speaks `Could not verify` without issuing a second web search.

`summarize` produces a concise English summary regardless of the language visible on screen. It translates the meaning while preserving important names, numbers, dates, qualifications, warnings, and action items.

Both commands send an intent to the already-running daemon and return after it accepts the request. A new request cancels any analysis or speech preparation already in progress. For manual image testing, `analyze --intent` accepts `auto`, `fact-check`, or `english-summary` and prints the result JSON without speaking it.

## Config

Default config path:

```text
~/.config/screen-agent/config.toml
```

Example:

```toml
capture_mode = "window"
screenshot_backend = "grim"
agent_provider = "gemini"
agent_model = "gemini-3.5-flash"
agent_max_output_tokens = 4096
agent_prompt_path = "~/workspace/golang2/apps/screen_agent/prompt.md"
tts_model = "gemini-3.1-flash-tts-preview"
tts_voice = "Kore"
tts_language_code = "en-US"
tts_player_command = "auto"
tts_timeout = "45s"
tts_playback_timeout = "2m"
speak_when_uncertain = false
max_spoken_chars = 300
debug = false
retain_debug_captures = false
debug_capture_dir = "~/.local/state/screen-agent/captures"
thinking_tone_enabled = true
thinking_tone_command = "auto"
thinking_tone_interval = "1s"
no_question_sound_enabled = true
no_question_sound_command = "auto"
```

`thinking_tone_command = "auto"` tries common local Linux sound commands such as `pw-play`, `paplay`, or `canberra-gtk-play`; on Windows it plays a short system sound from `%SystemRoot%\Media` through `ffplay` or PowerShell's `Media.SoundPlayer`. Set `thinking_tone_command` to a shell-free command string to override it, or set `thinking_tone_enabled = false`.

`no_question_sound_command = "auto"` plays a distinct one-shot sound when analysis completes and no answerable screen content is found. Set it to a shell-free command string to override it, or set `no_question_sound_enabled = false`.

`tts_player_command = "auto"` resolves to `pw-play`, `paplay`, `aplay`, or `ffplay` on Linux, and to `ffplay` or PowerShell's `Media.SoundPlayer` on Windows.

## Environment

Common overrides:

```sh
export SCREEN_AGENT_CAPTURE_MODE=full
export SCREEN_AGENT_AGENT_PROVIDER=gemini
export SCREEN_AGENT_AGENT_MODEL=gemini-3.5-flash
export SCREEN_AGENT_TTS_MODEL=gemini-3.1-flash-tts-preview
export SCREEN_AGENT_TTS_VOICE=Kore
```

By default, startup loads `.env` from the git repository root when the app is run from inside this checkout. Put the Gemini key there:

```env
GEMINI_API_KEY=...
```

Provider credentials are still ordinary environment variables, so an already-exported `GEMINI_API_KEY` takes precedence over `.env`.

If a cloud model is used, screenshots are sent to that provider. Screenshots are deleted after each hotkey event unless `retain_debug_captures` is enabled.

Window and monitor modes fail without taking a screenshot when Hyprland cannot provide the requested geometry. They never fall back to uploading the full desktop; use `capture_mode = "full"` only when that scope is intentional.

## Windows

Capture uses native GDI with per-monitor DPI awareness. `screenshot_backend` defaults to `"windows"` there and `full`, `monitor`, and `window` modes work without external tools; `region` mode is not supported. Window geometry comes from the foreground window's DWM extended frame bounds, so captures exclude drop shadows and invisible resize borders.

The daemon can register a system-wide hotkey itself — no external hotkey tool needed:

```toml
hotkey = "ctrl+alt+a"
```

Combos are `ctrl`/`alt`/`shift`/`win` plus one key (`a`–`z`, `0`–`9`, `f1`–`f24`, or named keys like `space`). Registration fails loudly at startup if another program already owns the combo. Function keys may be used without a modifier. When no hotkey is configured, trigger the daemon with:

```powershell
screen-agent assist       # auto assist (same as the hotkey)
screen-agent fact-check
screen-agent summarize
```

The runtime directory (PID file, control socket, transient captures) is `%LOCALAPPDATA%\Temp\screen-agent`. The control socket is an `AF_UNIX` socket, which Windows 10 1803+ supports natively. `SIGUSR1` does not exist on Windows; use the hotkey or the `assist` command instead. To start the daemon at login, add a shortcut to `screen-agent.exe daemon` in `shell:startup` or register a Task Scheduler logon task — or use the installer below, which sets this up.

### Installer

Build a Windows installer with [Inno Setup 6](https://jrsoftware.org/isinfo.php) (`winget install JRSoftware.InnoSetup`):

```powershell
powershell -File installer\build.ps1 -Version 0.1.0
```

The result is `installer\output\screen-agent-setup-<version>.exe`. It installs per-user (no admin required) into `%LOCALAPPDATA%\Programs\screen-agent` and:

- asks for the `GEMINI_API_KEY` (masked input) and stores it in `%USERPROFILE%\.config\screen-agent\.env`, which the app loads at startup; already-set environment variables still take precedence
- seeds `%USERPROFILE%\.config\screen-agent\config.toml` with `hotkey = "ctrl+shift+a"` and the installed prompt path, unless a config already exists
- optionally starts the daemon at login via a hidden-window launcher (`daemon-hidden.vbs`, no console window) and adds the app to the user `PATH`
- kills a running daemon before upgrading, and removes the PATH entry and startup shortcut on uninstall; the config and API key are kept

Unattended install: `screen-agent-setup-<version>.exe /VERYSILENT /ApiKey=<key>`.

## Hyprland

Add a binding manually, for example:

```text
bind = SUPER SHIFT, A, exec, sh -c 'kill -USR1 "$(cat "$XDG_RUNTIME_DIR/screen-agent/screen-agent.pid")"'
bind = CTRL SHIFT, F, exec, sh -c '"$HOME/.local/bin/screen-agent" fact-check'
bind = CTRL SHIFT, Z, exec, sh -c '"$HOME/.local/bin/screen-agent" summarize'
```

Then reload and check config errors:

```sh
hyprctl reload
hyprctl configerrors
```

Runtime dependencies for the default path on Linux are `grim`, `hyprctl`, a WAV-capable audio player such as `pw-play` or `paplay`, and Gemini credentials. Region capture additionally needs `slurp`. On Windows the only runtime dependency is Gemini credentials.

The app must already be running before any binding fires. The systemd user service above is the intended setup.
