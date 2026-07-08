# pupbox

Pupbox is a Mac-first prototype for a voice-only conversational plush dog. The short-term goal is to let a 3-year-old child talk with a toy without looking at a screen. The browser UI is only a prototype shell and parent/debug surface; the intended product shape is a plush toy with a button, microphone, speaker, and optional simple motion.

## Current Features

- Go HTTP backend with no third-party Go dependencies.
- Parent/debug page with transcript and activity buttons.
- Child-facing `toy.html` mode that simulates a single-button plush toy.
- Mock mode that runs without an OpenAI API key.
- Chrome Web Speech fallback in mock mode for low-cost local voice testing.
- OpenAI mode for STT, model response, and TTS.
- Deterministic activity engine for toddler-friendly interactions.
- Safety rules that intercept dangerous or private topics before model calls.
- Hardware action names in activity responses, ready to map to tail/LED/motion control.

## Interaction Model

The child-facing flow is intentionally simple:

1. Press once to wake the dog.
2. Press and hold to speak.
3. Release to send.
4. Listen to the reply.

The model should not depend on the child giving complete answers. Inputs like `嗯嗯`, `啊呀`, and `汪汪` are treated as valid interaction signals and routed to simple activities such as clapping, counting, or comfort.

## Project Layout

```text
cmd/pupbox-server/       Go server entrypoint
internal/dog/            persona, safety rules, activities, hardware action names
internal/openaiapi/      direct OpenAI HTTP client
internal/server/         HTTP API and static file serving
web/static/index.html    parent/debug UI
web/static/toy.html      child-facing toy-mode UI
Makefile                 repeatable local commands
AGENTS.md                Codex guidance for future agents
```

## Run

Mock mode, no OpenAI key required:

```bash
make dev-mock
```

OpenAI mode:

```bash
export OPENAI_API_KEY=...
make dev-openai
```

By default the Makefile uses:

```text
http://127.0.0.1:8791
```

Open:

```text
http://127.0.0.1:8791/toy.html
```

Parent/debug page:

```text
http://127.0.0.1:8791/
```

Override the port:

```bash
make dev-openai PUPBOX_ADDR=127.0.0.1:8792
```

## OpenAI Settings

Keep secrets in your shell environment only:

```bash
export OPENAI_API_KEY=...
```

Optional settings:

```bash
export PUPBOX_CHAT_MODEL=gpt-4o-mini
export PUPBOX_STT_MODEL=whisper-1
export PUPBOX_TTS_MODEL=gpt-4o-mini-tts
export PUPBOX_TTS_VOICE=marin
export PUPBOX_TTS_FORMAT=mp3
```

Optional TTS style prompt:

```bash
export PUPBOX_TTS_PROMPT='你是一个藏在毛绒小狗玩具里的中文声音。声音要温暖、圆润、亲近、像在和三岁小女孩玩；语速偏慢，吐字清楚，句子之间有短停顿。不要播音腔，不要机械，不要严肃。'
```

Never commit `.env`, API keys, recordings, transcripts, or private family data.

## API

```text
GET  /api/health
GET  /api/activities
POST /api/chat   {"text":"豆豆讲故事"}
POST /api/speech {"text":"汪，豆豆醒啦"}
POST /api/voice  multipart/form-data audio=<recording>
```

`POST /api/chat` synthesizes TTS in OpenAI mode unless `tts=off` is set:

```bash
curl -sS -H 'Content-Type: application/json' \
  -d '{"text":"嗯嗯"}' \
  'http://127.0.0.1:8791/api/chat?tts=off'
```

## Local Automation

```bash
make test-local       # go test + go build, no network API calls
make test-openai-api  # local API smoke tests with tts=off
make test-ui          # opens toy.html and checks browser console
make dev-openai       # starts fixed-port OpenAI mode, requires OPENAI_API_KEY
make dev-mock         # starts fixed-port mock mode
```

Routine smoke tests use `tts=off` so they do not spend TTS quota.

## Safety Rules

Pupbox intercepts safety-sensitive topics before calling the model. Current rule groups include:

- injury or pain
- fire, electricity, batteries, knives, medicine, doors, windows, balconies
- strangers, getting lost, leaving home
- address, phone number, kindergarten, parent names, and other private information

For these cases, the dog tells the child to find a parent or caregiver.

## Hardware Direction

Do not start by building a walking robot. The recommended path is incremental:

1. Mac runs the brain; speaker is near or inside the plush dog.
2. A removable voice box goes inside a plush dog with a zipper pocket.
3. The voice box contains microphone, speaker, physical button, power switch, and status LED.
4. Add one simple motion first, such as tail wagging or a breathing light.
5. Use symbolic action names from the API, such as `tail_wag` or `slow_breathe`, and let hardware firmware enforce angle, speed, and duration limits.

Avoid exposed batteries, loose wiring, loose screws, detachable small parts, and button-cell batteries.

## Current Limitations

- Real toddler speech recognition still needs hands-on testing with the child or representative recordings.
- OpenAI TTS depends on API quota and billing status.
- Browser speech synthesis in mock mode is only a fallback and may sound poor.
- The project does not yet persist parent settings or conversation logs.
