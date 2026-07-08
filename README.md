# pupbox

Pupbox is a Mac-first prototype for a voice-only conversational plush dog. The short-term goal is to let a 3-year-old child talk with a toy without looking at a screen. The browser UI is only a prototype shell and parent/debug surface; the intended product shape is a plush toy with a button, microphone, speaker, and optional simple motion.

## Current Features

- Go HTTP backend with no third-party Go dependencies.
- Parent/debug page with transcript and activity buttons.
- Child-facing `toy.html` mode that simulates a single-button plush toy.
- Mock mode that runs without an OpenAI API key.
- Chrome Web Speech fallback in mock mode for low-cost local voice testing.
- OpenAI mode for model response and optional STT/TTS.
- Alibaba Cloud DashScope mode for lower-cost Chinese STT/TTS.
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
internal/dashscopeapi/   direct DashScope HTTP client for Qwen-ASR and CosyVoice
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

Alibaba Cloud DashScope voice mode:

```bash
export CHAT_ARCHIVE_QWEN_API_KEY=...
make dev-dashscope
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
export PUPBOX_TTS_SPEED=0.88
```

Optional TTS style prompt:

```bash
export PUPBOX_TTS_PROMPT='你是一个藏在毛绒小狗玩具里的中文声音。声音要温暖、圆润、亲近、像在和三岁小女孩玩；语速偏慢，吐字清楚，句子之间有短停顿。不要播音腔，不要机械，不要严肃。'
```

### Voice Tuning

First confirm that the toy page is not using browser fallback speech:

```bash
curl -sS http://127.0.0.1:8791/api/health
```

The response should include:

```json
{
  "mode": "openai",
  "tts_model": "gpt-4o-mini-tts",
  "tts_voice": "marin",
  "tts_speed": 0.88
}
```

If `mode` is `mock`, or if the response contains a `tts_error`, the browser may fall back to local speech synthesis, which usually sounds worse.

Useful tuning options:

```bash
export PUPBOX_TTS_VOICE=cedar
export PUPBOX_TTS_SPEED=0.82
export PUPBOX_TTS_PROMPT='你是一个藏在毛绒小狗玩具里的中文小狗声音。语气温柔、开心、像贴近耳边说话；语速慢一点，句子短一点，停顿自然。不要播音腔，不要夸张表演。'
make dev-openai
```

Try `marin` first, then `cedar`, then compare other voices if needed. For a plush toy, the first goal is not maximum realism; it is whether the child finds the voice friendly, clear, and not startling.

## DashScope Voice Settings

DashScope mode uses Alibaba Cloud Model Studio / DashScope for STT and TTS, while chat can still fall back to deterministic activities and mock replies if OpenAI is unavailable.

```bash
export CHAT_ARCHIVE_QWEN_API_KEY=...
make dev-dashscope
```

Defaults:

```bash
export PUPBOX_VOICE_PROVIDER=dashscope
export PUPBOX_DASHSCOPE_BASE_URL=https://dashscope.aliyuncs.com
export PUPBOX_DASHSCOPE_CHAT_MODEL=qwen-turbo
export PUPBOX_DASHSCOPE_STT_MODEL=qwen3-asr-flash
export PUPBOX_DASHSCOPE_TTS_MODEL=cosyvoice-v3-flash
export PUPBOX_DASHSCOPE_TTS_VOICE=longhuhu_v3
export PUPBOX_DASHSCOPE_TTS_FORMAT=mp3
export PUPBOX_DASHSCOPE_TTS_SPEED=0.88
export PUPBOX_DASHSCOPE_TTS_SAMPLE_RATE=24000
```

`make dev-dashscope` defaults to `PUPBOX_CHAT_PROVIDER=dashscope`, so STT, TTS, and free-form fallback replies all use DashScope/Qwen. To use only deterministic activities and local mock fallback replies:

```bash
make dev-dashscope DASHSCOPE_CHAT_PROVIDER=mock
```

To use OpenAI for non-deterministic replies while keeping DashScope STT/TTS:

```bash
make dev-dashscope DASHSCOPE_CHAT_PROVIDER=openai
```

Pupbox still uses deterministic activity routing before Qwen. That means safety rules and known toddler workflows, such as `讲故事`, `数数`, `猜动物`, `插座`, `嗯嗯`, and `汪汪`, are handled by local code first. Qwen is only called when no local rule or activity matches.

The default TTS combination is `cosyvoice-v3-flash + longhuhu_v3` because it was verified against the live DashScope API. `cosyvoice-v3.5-flash` is supported as a configurable model, but the currently tested `longhuhu_v3` and `longxiaochun` voices returned engine error `418` with that model, so it is not the default yet.

`PUPBOX_DASHSCOPE_TTS_PROMPT` is intentionally empty by default. The same verified voice returned engine errors when a default instruction was sent. Add a prompt only after testing the chosen model and voice combination.

If your DashScope workspace requires the newer workspace-specific domain, set:

```bash
export PUPBOX_DASHSCOPE_BASE_URL=https://<WorkspaceId>.cn-beijing.maas.aliyuncs.com
```

The browser records 16 kHz mono WAV audio before upload so Qwen-ASR can receive `data:audio/wav;base64,...` input directly. This avoids needing a public recording URL and reduces upload size. Recordings shorter than about 260 ms are ignored locally instead of being sent to STT.

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

Voice and chat responses include timing diagnostics:

```json
{
  "timings": {
    "total_ms": 2860,
    "stt_ms": 640,
    "reply_ms": 0,
    "tts_ms": 2100,
    "audio_bytes": 18244
  }
}
```

## Local Automation

```bash
make test-local       # go test + go build, no network API calls
make test-openai-api  # local API smoke tests with tts=off
make test-ui          # opens toy.html and checks browser console
make dev-openai       # starts fixed-port OpenAI mode, requires OPENAI_API_KEY
make dev-dashscope    # starts fixed-port DashScope voice mode, requires CHAT_ARCHIVE_QWEN_API_KEY or DASHSCOPE_API_KEY
make dev-mock         # starts fixed-port mock mode
make check-secrets    # scans for obvious committed secrets before pushing
```

Routine smoke tests use `tts=off` so they do not spend TTS quota.

## Parent Validation Checklist

Use `http://127.0.0.1:8791/toy.html` for child-facing validation.

1. Confirm the page says `OpenAI` or `阿里云` and shows the expected voice and speed.
2. Tap once to wake the dog and check whether the wake voice sounds like server TTS, not browser speech.
3. Press and hold, say `嗯嗯` or another unclear toddler-like sound, then release. The dog should still respond with a simple playful activity.
4. Say `豆豆讲故事`. The reply should be short enough to finish before the child loses attention.
5. Say `我想玩插座`. The dog should route to a caregiver safety reply.
6. Check whether the child understands the reply without looking at the screen.
7. Note latency, volume, voice preference, recognition errors, and any reply that feels too long or too adult.

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
