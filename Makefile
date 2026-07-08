PUPBOX_ADDR ?= 127.0.0.1:8791
PUPBOX_BASE_URL ?= http://$(PUPBOX_ADDR)
CODEX_HOME ?= $(HOME)/.codex
PWCLI ?= $(CODEX_HOME)/skills/playwright/scripts/playwright_cli.sh
DASHSCOPE_CHAT_PROVIDER ?= dashscope

.PHONY: test-local test-openai-api test-ui dev-openai dev-dashscope dev-mock check-openai-key check-dashscope-key check-secrets

test-local:
	go test ./...
	go build ./...

test-openai-api:
	curl -sS "$(PUPBOX_BASE_URL)/api/health"
	curl -sS "$(PUPBOX_BASE_URL)/api/activities"
	curl -sS -H 'Content-Type: application/json' -d '{"text":"嗯嗯"}' "$(PUPBOX_BASE_URL)/api/chat?tts=off"
	curl -sS -H 'Content-Type: application/json' -d '{"text":"豆豆讲故事"}' "$(PUPBOX_BASE_URL)/api/chat?tts=off"
	curl -sS -H 'Content-Type: application/json' -d '{"text":"我想玩插座"}' "$(PUPBOX_BASE_URL)/api/chat?tts=off"

test-ui:
	env CODEX_HOME="$(CODEX_HOME)" bash "$(PWCLI)" open "$(PUPBOX_BASE_URL)/toy.html"
	env CODEX_HOME="$(CODEX_HOME)" bash "$(PWCLI)" snapshot
	env CODEX_HOME="$(CODEX_HOME)" bash "$(PWCLI)" console

dev-openai: check-openai-key
	env -u PUPBOX_MODE PUPBOX_CHAT_PROVIDER=openai PUPBOX_VOICE_PROVIDER=openai PUPBOX_ADDR="$(PUPBOX_ADDR)" go run ./cmd/pupbox-server

dev-dashscope: check-dashscope-key
	env -u PUPBOX_MODE PUPBOX_CHAT_PROVIDER="$(DASHSCOPE_CHAT_PROVIDER)" PUPBOX_VOICE_PROVIDER=dashscope PUPBOX_ADDR="$(PUPBOX_ADDR)" go run ./cmd/pupbox-server

dev-mock:
	env PUPBOX_MODE=mock PUPBOX_ADDR="$(PUPBOX_ADDR)" go run ./cmd/pupbox-server

check-openai-key:
	@test -n "$$OPENAI_API_KEY" || (echo "OPENAI_API_KEY is not set"; exit 1)

check-dashscope-key:
	@test -n "$$CHAT_ARCHIVE_QWEN_API_KEY" || test -n "$$DASHSCOPE_API_KEY" || (echo "CHAT_ARCHIVE_QWEN_API_KEY or DASHSCOPE_API_KEY is not set"; exit 1)

check-secrets:
	@! rg -n --hidden --glob '!.git/**' --glob '!*.sum' --glob '!*.lock' '(sk-[A-Za-z0-9_-]{20,}|LTAI[A-Za-z0-9]{16,}|[O]SSAccessKeyId=|SSO[_A-Z]*TOKEN|BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY)' .
