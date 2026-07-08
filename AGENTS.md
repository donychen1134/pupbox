# Pupbox Agent Guidance

## Project Goal

Pupbox is a voice-first prototype for putting a conversational dog into a plush toy for a 3-year-old child. The child-facing experience should not require reading or looking at a screen. Browser pages are prototype shells and parent/debug tools; the intended product interaction is a physical button, microphone, speaker, and optional simple motion.

## Naming

Use `AGENTS.md` for durable Codex guidance. Do not create `AGENT.md` unless a specific external tool requires it.

## Safety And Privacy

- Never commit API keys, SSO tokens, `.env` files, recordings, transcripts, or private child/family data.
- Treat any pasted API key as compromised and ask the user to rotate it.
- Before pushing to a remote, run a sensitive-information check such as `make check-secrets`. If a potential secret or private child/family artifact is found, stop and ask the user before pushing.
- Keep routine tests on `tts=off` unless the task explicitly requires OpenAI TTS verification.
- Do not add continuous background listening by default. Prefer press-and-hold or a physical button.
- Child-facing replies must be short, gentle, and concrete.
- The assistant must not ask the child for address, phone number, kindergarten, parent names, or other private information.
- Dangerous topics must route to a parent/caregiver response, especially injury, pain, fire, electricity, medicine, windows, doors, strangers, and leaving home.
- Hardware suggestions must avoid exposed batteries, loose wires, loose screws, small detachable parts, and button-cell batteries.

## Architecture

- `cmd/pupbox-server`: Go HTTP server entrypoint.
- `internal/dog`: child-safe persona, activity routing, safety rules, and future hardware action names.
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
export PUPBOX_CHAT_MODEL=gpt-4o-mini
export PUPBOX_STT_MODEL=whisper-1
export PUPBOX_TTS_MODEL=gpt-4o-mini-tts
export PUPBOX_TTS_VOICE=marin
export PUPBOX_TTS_FORMAT=mp3
export PUPBOX_TTS_SPEED=0.88
```

Do not write real key values into docs, examples, logs, screenshots, or commits.

## Development Notes

- Keep activity routing deterministic before falling back to free-form model responses.
- Prefer adding reviewed content and activities over making the model more open-ended.
- Keep future hardware actions as stable symbolic names such as `tail_wag`, `glow_red`, or `slow_breathe`; do not let model output directly control motors or PWM.
- In OpenAI mode, `POST /api/chat` may synthesize TTS unless `tts=off` is set.
- Use `toy.html` for child-facing flow verification and `index.html` for parent/debug verification.

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
