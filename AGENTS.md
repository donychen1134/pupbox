# Pupbox Agent Guidance

## Project Goal

Pupbox is a voice-first prototype for putting a conversational dog into a plush toy for a 3-year-old child. The child-facing experience should not require reading or looking at a screen. Browser pages are prototype shells and parent/debug tools; the intended product interaction is a physical button, microphone, speaker, and optional simple motion.

## Naming

Use `AGENTS.md` for durable Codex guidance. Do not create `AGENT.md` unless a specific external tool requires it.

## Safety And Privacy

- Never commit API keys, SSO tokens, `.env` files, recordings, transcripts, or private child/family data.
- Treat any pasted API key as compromised and ask the user to rotate it.
- Before pushing to a remote, run a sensitive-information check such as `make check-secrets`. If a potential secret or private child/family artifact is found, stop and ask the user before pushing.
- When exposing Pupbox beyond localhost, require `PUPBOX_ACCESS_TOKEN`; do not publish chat, speech, voice, or activity APIs without access-token protection.
- Generate `PUPBOX_ACCESS_TOKEN` with URL-safe characters, such as `openssl rand -hex 32`; raw base64 tokens can contain `+`, `/`, or `=` and break `?token=...` links unless encoded.
- For VPS deployment, do not upload files from the local workstation. Build release artifacts in GitHub Actions and install them on the VPS from GitHub Releases.
- Event logs are for parent diagnostics only. Do not store audio bytes, access tokens, API keys, IP addresses, or private child/family data in event records.
- Audio recording playback must be opt-in diagnostics only. Keep short retention, protect it with `PUPBOX_ACCESS_TOKEN`, and never commit recording files.
- Keep routine tests on `tts=off` unless the task explicitly requires OpenAI TTS verification.
- Do not add continuous background listening by default. Prefer press-and-hold or a physical button.
- Child-facing replies must be short, gentle, and concrete.
- The assistant must not ask the child for address, phone number, kindergarten, parent names, or other private information.
- Dangerous topics must route to a parent/caregiver response, especially injury, pain, fire, electricity, medicine, windows, doors, strangers, and leaving home.
- Hardware suggestions must avoid exposed batteries, loose wires, loose screws, small detachable parts, and button-cell batteries.

## Architecture

- `cmd/pupbox-server`: Go HTTP server entrypoint.
- `internal/dog`: child-safe persona, activity routing, safety rules, and future hardware action names.
- `internal/dashscopeapi`: direct DashScope client for Qwen-ASR and CosyVoice.
- `internal/openaiapi`: direct OpenAI HTTP client for Responses, STT, and TTS.
- `internal/server`: HTTP API and static file serving.
- `web/static/index.html`: parent/debug UI with transcript and activity buttons.
- `web/static/toy.html`: child-facing toy mode that simulates a single-button plush toy.

## Local Commands

Use the Makefile targets for repeatable testing and development:

```bash
make test-local
make test-openai-api
make test-ui
make dev-openai
make dev-dashscope
make dev-mock
```

`make test-openai-api` intentionally uses `tts=off` for chat calls to avoid spending TTS quota during routine smoke tests.

## OpenAI Configuration

Set secrets only in the shell environment:

```bash
export OPENAI_API_KEY=...
```

Useful optional settings:

```bash
export PUPBOX_ADDR=127.0.0.1:8791
export PUPBOX_ACCESS_TOKEN=...
export PUPBOX_EVENT_LOG_PATH=data/events.jsonl
export PUPBOX_EVENT_LOG_LIMIT=500
export PUPBOX_TTS_CACHE_DIR=data/tts-cache
export PUPBOX_TTS_CACHE_LIMIT=512
export PUPBOX_TTS_PREWARM=true
export PUPBOX_TTS_PREWARM_LIMIT=32
export PUPBOX_CHAT_MODEL=gpt-4o-mini
export PUPBOX_STT_MODEL=whisper-1
export PUPBOX_TTS_MODEL=gpt-4o-mini-tts
export PUPBOX_TTS_VOICE=marin
export PUPBOX_TTS_FORMAT=mp3
export PUPBOX_TTS_SPEED=0.88
```

## DashScope Configuration

DashScope voice mode reads `CHAT_ARCHIVE_QWEN_API_KEY` first and falls back to `DASHSCOPE_API_KEY`.

Useful optional settings:

```bash
export PUPBOX_VOICE_PROVIDER=dashscope
export PUPBOX_DASHSCOPE_CHAT_MODEL=qwen-turbo
export PUPBOX_DASHSCOPE_STT_MODEL=qwen3-asr-flash
export PUPBOX_DASHSCOPE_TTS_MODEL=cosyvoice-v3-flash
export PUPBOX_DASHSCOPE_TTS_VOICE=longhuhu_v3
```

Do not send a default `PUPBOX_DASHSCOPE_TTS_PROMPT`. Live smoke tests showed `cosyvoice-v3-flash + longhuhu_v3` succeeds without `instruction`, while the same request can return CosyVoice engine errors when a default instruction is included.

`make dev-dashscope` defaults to `PUPBOX_CHAT_PROVIDER=dashscope` through `DASHSCOPE_CHAT_PROVIDER=dashscope`, so STT, TTS, and free-form fallback replies all use DashScope/Qwen. Use `make dev-dashscope DASHSCOPE_CHAT_PROVIDER=mock` to disable model fallback, or `make dev-dashscope DASHSCOPE_CHAT_PROVIDER=openai` only when OpenAI API quota is available.

Do not write real key values into docs, examples, logs, screenshots, or commits.

## Development Notes

- Keep activity routing deterministic before falling back to free-form model responses.
- Prefer adding reviewed content and activities over making the model more open-ended.
- In DashScope mode, deterministic routing means safety checks and known activities run before Qwen; Qwen should only handle unmatched free-form child input.
- Keep future hardware actions as stable symbolic names such as `tail_wag`, `glow_red`, or `slow_breathe`; do not let model output directly control motors or PWM.
- In server voice mode, `POST /api/chat` may synthesize TTS unless `tts=off` is set.
- API responses should keep the `timings` object for latency diagnosis.
- Keep `/api/speech-stream` additive and preserve complete-audio fallback; the parent diagnostics page should not depend on browser PCM streaming.
- Browser microphone uploads should stay in 16 kHz mono WAV unless a provider-specific reason requires another format.
- Use `toy.html` for child-facing flow verification and `index.html` for parent/debug verification.

## Completion Notes

At the end of each work turn, tell the user:

- What was verified by the agent, preferring direct local validation when possible.
- What the user should manually verify next, especially child-facing voice behavior that requires listening.
- The next most useful project step toward the voice-only plush dog goal.

## Verification Before Commit

At minimum run:

```bash
make test-local
```

When the server is already running on the configured port, also run:

```bash
make test-openai-api
make test-ui
make check-secrets
```
