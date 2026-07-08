# Deploy Pupbox On A VPS

This guide runs Pupbox as a protected HTTPS service so an iPhone browser can open `toy.html` from anywhere.

## Target Shape

```text
iPhone Safari
  -> https://pupbox.example.com/toy.html?token=<access-token>
  -> Caddy HTTPS reverse proxy
  -> pupbox-server on 127.0.0.1:8791
  -> DashScope STT / Qwen / TTS
```

Do not expose Pupbox without `PUPBOX_ACCESS_TOKEN`. The chat and voice endpoints can spend provider quota.

## Build

On the VPS:

```bash
git clone https://github.com/donychen1134/pupbox.git /opt/pupbox
cd /opt/pupbox
go build -o /opt/pupbox/pupbox-server ./cmd/pupbox-server
```

## Environment

Create `/etc/pupbox/pupbox.env`:

```bash
PUPBOX_ADDR=127.0.0.1:8791
PUPBOX_CHAT_PROVIDER=dashscope
PUPBOX_VOICE_PROVIDER=dashscope
PUPBOX_ACCESS_TOKEN=<generate-a-long-random-token>
CHAT_ARCHIVE_QWEN_API_KEY=<dashscope-api-key>
PUPBOX_DASHSCOPE_CHAT_MODEL=qwen-turbo
PUPBOX_DASHSCOPE_STT_MODEL=qwen3-asr-flash
PUPBOX_DASHSCOPE_TTS_MODEL=cosyvoice-v3-flash
PUPBOX_DASHSCOPE_TTS_VOICE=longhuhu_v3
PUPBOX_DASHSCOPE_TTS_SPEED=0.88
```

Keep this file readable only by the service user or root:

```bash
chmod 600 /etc/pupbox/pupbox.env
```

Generate a token with a command such as:

```bash
openssl rand -base64 32
```

## systemd

Create `/etc/systemd/system/pupbox.service`:

```ini
[Unit]
Description=Pupbox voice toy server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/pupbox
EnvironmentFile=/etc/pupbox/pupbox.env
ExecStart=/opt/pupbox/pupbox-server
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Start it:

```bash
systemctl daemon-reload
systemctl enable --now pupbox
systemctl status pupbox
```

Check logs:

```bash
journalctl -u pupbox -f
```

## HTTPS With Caddy

Install Caddy and create a Caddyfile:

```caddyfile
pupbox.example.com {
	reverse_proxy 127.0.0.1:8791
}
```

Reload Caddy:

```bash
caddy reload --config /etc/caddy/Caddyfile
```

## Smoke Test

The API should reject requests without a token:

```bash
curl -i https://pupbox.example.com/api/health
```

The same endpoint should work with the token:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  https://pupbox.example.com/api/health
```

Open the toy page on the iPhone:

```text
https://pupbox.example.com/toy.html?token=<access-token>
```

The page stores the token in browser local storage and removes it from the address bar after the first load. To clear the stored token:

```text
https://pupbox.example.com/toy.html?clearToken=1
```

## Operational Notes

- Rotate `PUPBOX_ACCESS_TOKEN` if the URL is shared accidentally.
- Do not paste real API keys or tokens into issue trackers, screenshots, docs, or commits.
- Keep routine tests on `tts=off` unless you explicitly want to spend TTS quota.
- Start with browser validation before building an iOS app; this keeps the product risk focused on the child voice interaction.
