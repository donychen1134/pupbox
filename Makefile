PUPBOX_ADDR ?= 127.0.0.1:8791
PUPBOX_BASE_URL ?= http://$(PUPBOX_ADDR)
CODEX_HOME ?= $(HOME)/.codex
PWCLI ?= $(CODEX_HOME)/skills/playwright/scripts/playwright_cli.sh

.PHONY: test-local test-openai-api test-ui dev-openai dev-mock check-openai-key check-secrets

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
	env -u PUPBOX_MODE PUPBOX_ADDR="$(PUPBOX_ADDR)" go run ./cmd/pupbox-server

dev-mock:
	env PUPBOX_MODE=mock PUPBOX_ADDR="$(PUPBOX_ADDR)" go run ./cmd/pupbox-server

check-openai-key:
	@test -n "$$OPENAI_API_KEY" || (echo "OPENAI_API_KEY is not set"; exit 1)

check-secrets:
	@! rg -n --hidden --glob '!.git/**' --glob '!*.sum' --glob '!*.lock' '(sk-(proj|live|test)-[A-Za-z0-9_-]{20,}|SSO[_A-Z]*TOKEN|BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY)' .
