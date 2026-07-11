# Deploy Pupbox On A VPS

This guide runs Pupbox as a protected HTTPS service so an iPhone browser can open `toy.html` from anywhere.

## Target Shape

```text
iPhone Safari
  -> https://pupbox.983457.xyz/toy.html?token=<access-token>
  -> Caddy HTTPS reverse proxy
  -> pupbox-server on 127.0.0.1:8791
  -> DashScope STT / Qwen / TTS
```

Do not expose Pupbox without `PUPBOX_ACCESS_TOKEN`. The chat and voice endpoints can spend provider quota.

## DNS

Use a separate subdomain:

```text
pupbox.983457.xyz A 140.238.39.118
```

The existing `ora1.983457.xyz` configuration does not need to change. If the domain already has a wildcard record such as `*.983457.xyz`, no extra DNS record may be needed; verify first:

```bash
dig +short pupbox.983457.xyz A
```

The result should include the VPS public IP.

## Install From GitHub Release

Do not upload files from a local machine. The VPS should download release artifacts from GitHub.

Create a release by pushing a tag from the repository:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions builds:

```text
pupbox-linux-amd64.tar.gz
pupbox-linux-arm64.tar.gz
checksums.txt
```

On the VPS, install a release:

```bash
release_tag=v0.1.8
curl -fsSL "https://raw.githubusercontent.com/donychen1134/pupbox/${release_tag}/scripts/install-release.sh" \
  -o /tmp/install-pupbox-release.sh
sudo bash /tmp/install-pupbox-release.sh "${release_tag}"
```

The script detects `amd64` or `arm64`, downloads the matching tarball and `checksums.txt` from GitHub Releases, verifies SHA-256, extracts it under `/opt/pupbox/releases/<tag>`, and updates `/opt/pupbox/current`.

The release package includes:

```text
pupbox-server
web/static/
README.md
docs/
```

## Optional Swap

The prototype can run in about 1 GiB memory because STT, Qwen, and TTS run in DashScope. Add swap to avoid memory pressure during package installation or OS maintenance:

```bash
sudo fallocate -l 1G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
free -h
```

## Environment

Create `/etc/pupbox/pupbox.env`:

```bash
PUPBOX_ADDR=127.0.0.1:8791
PUPBOX_CHAT_PROVIDER=dashscope
PUPBOX_VOICE_PROVIDER=dashscope
PUPBOX_ACCESS_TOKEN=<generate-a-url-safe-random-token>
PUPBOX_EVENT_LOG_PATH=/var/lib/pupbox/events.jsonl
PUPBOX_EVENT_LOG_LIMIT=500
PUPBOX_TTS_CACHE_DIR=/var/lib/pupbox/tts-cache
PUPBOX_TTS_CACHE_LIMIT=512
PUPBOX_TTS_PREWARM=true
PUPBOX_TTS_PREWARM_LIMIT=48
# Optional parent-only diagnostic playback. Keep short retention.
PUPBOX_RECORDING_DIR=/var/lib/pupbox/recordings
PUPBOX_RECORDING_LIMIT=20
CHAT_ARCHIVE_QWEN_API_KEY=<dashscope-api-key>
PUPBOX_DASHSCOPE_CHAT_MODEL=qwen-plus-character
PUPBOX_DASHSCOPE_STT_MODEL=qwen3-asr-flash
PUPBOX_DASHSCOPE_TTS_MODEL=cosyvoice-v3-flash
PUPBOX_DASHSCOPE_TTS_VOICE=longhuhu_v3
PUPBOX_DASHSCOPE_TTS_FORMAT=opus
PUPBOX_DASHSCOPE_TTS_SPEED=0.88
```

Keep this file readable only by the service user or root:

```bash
chmod 600 /etc/pupbox/pupbox.env
```

Create a dedicated service user and data directories:

```bash
sudo useradd --system --home /var/lib/pupbox --shell /usr/sbin/nologin pupbox 2>/dev/null || true
sudo install -d -o pupbox -g pupbox -m 700 /var/lib/pupbox /var/lib/pupbox/recordings /var/lib/pupbox/tts-cache
```

Generate a token with a command such as:

```bash
openssl rand -hex 32
```

Use a URL-safe token because the phone entrypoint passes it through `?token=...`. Raw base64 tokens can contain `+`, `/`, or `=`, which are easy to mishandle in URLs unless encoded.

## systemd

Create `/etc/systemd/system/pupbox.service`:

```ini
[Unit]
Description=Pupbox voice toy server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=pupbox
Group=pupbox
WorkingDirectory=/opt/pupbox/current
EnvironmentFile=/etc/pupbox/pupbox.env
ExecStart=/opt/pupbox/current/pupbox-server
Restart=always
RestartSec=3
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/pupbox

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

Keep the generated root Caddyfile intact. This server imports `/etc/caddy/sites/*.conf`, so add only a new site file:

```bash
sudo mkdir -p /etc/caddy/sites
sudo nano /etc/caddy/sites/pupbox.conf
```

Write:

```caddyfile
pupbox.983457.xyz {
	reverse_proxy 127.0.0.1:8791
}
```

Validate and reload Caddy without restarting v2ray:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
sudo systemctl status caddy --no-pager
sudo systemctl status v2ray --no-pager
```

## Smoke Test

The API should reject requests without a token:

```bash
curl -i https://pupbox.983457.xyz/api/health
```

The same endpoint should work with the token:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  https://pupbox.983457.xyz/api/health
```

The diagnostics API should return recent events after a few conversations:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  'https://pupbox.983457.xyz/api/events?limit=50'
```

Open the toy page on the iPhone:

```text
https://pupbox.983457.xyz/toy.html?token=<access-token>
```

The page stores the token in browser local storage and removes it from the address bar after the first load. To clear the stored token:

```text
https://pupbox.983457.xyz/toy.html?clearToken=1
```

## Operational Notes

- Rotate `PUPBOX_ACCESS_TOKEN` if the URL is shared accidentally.
- Do not paste real API keys or tokens into issue trackers, screenshots, docs, or commits.
- Do not store audio recordings by default. If `PUPBOX_RECORDING_DIR` is enabled for parent diagnostics, keep short retention and protect the site with `PUPBOX_ACCESS_TOKEN`.
- The JSONL event log stores text transcripts, replies, routes, timings, provider errors, and recording availability flags. `PUPBOX_EVENT_LOG_LIMIT` bounds retention by event count. It must not store audio bytes, API keys, access tokens, session IDs, or client IPs.
- The TTS cache stores generated reply audio under hashed filenames with private permissions. `PUPBOX_TTS_CACHE_LIMIT` bounds the number of files, and changing provider, model, voice, format, or speed automatically uses different cache keys.
- Keep routine tests on `tts=off` unless you explicitly want to spend TTS quota.
- Start with browser validation before building an iOS app; this keeps the product risk focused on the child voice interaction.
