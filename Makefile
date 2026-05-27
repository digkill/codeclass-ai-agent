.PHONY: build run dev tidy \
        daemon-start daemon-stop daemon-restart daemon-status daemon-logs \
        install-linux uninstall-linux

BINARY   := ai-agent
PLIST    := com.codeclass.ai-agent
PLIST_DST := $(HOME)/Library/LaunchAgents/$(PLIST).plist

# ── Build ─────────────────────────────────────────────────────────────────────

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

dev:
	go run .

tidy:
	GOPROXY="https://goproxy.cn,direct" go mod tidy

# ── macOS launchd (local dev) ─────────────────────────────────────────────────

daemon-install: build
	cp deploy/$(PLIST).plist $(PLIST_DST)
	launchctl load -w $(PLIST_DST)
	@echo "Installed and started: $(PLIST)"

daemon-start:
	launchctl start $(PLIST)

daemon-stop:
	launchctl stop $(PLIST)

daemon-restart: daemon-stop daemon-start

daemon-status:
	launchctl list | grep $(PLIST) || echo "not loaded"

daemon-logs:
	tail -f /usr/local/var/log/codeclass-ai-agent.log

daemon-uninstall:
	launchctl unload -w $(PLIST_DST) 2>/dev/null || true
	rm -f $(PLIST_DST)
	@echo "Removed: $(PLIST)"

# ── Linux systemd (production) ────────────────────────────────────────────────
# Usage: make install-linux DEPLOY_HOST=user@your-server

DEPLOY_HOST ?= root@82.202.128.182
DEPLOY_DIR  := /opt/codeclass-ai-agent

install-linux: build
	ssh $(DEPLOY_HOST) "mkdir -p $(DEPLOY_DIR)"
	scp $(BINARY) $(DEPLOY_HOST):$(DEPLOY_DIR)/$(BINARY)
	scp deploy/codeclass-ai-agent.service $(DEPLOY_HOST):/etc/systemd/system/
	@if [ -f .env ]; then \
		scp .env $(DEPLOY_HOST):$(DEPLOY_DIR)/.env && echo ".env uploaded"; \
	else \
		echo "WARNING: no local .env found — create $(DEPLOY_DIR)/.env on the server manually"; \
	fi
	ssh $(DEPLOY_HOST) " \
		chmod +x $(DEPLOY_DIR)/$(BINARY) && \
		systemctl daemon-reload && \
		systemctl enable codeclass-ai-agent && \
		systemctl restart codeclass-ai-agent && \
		systemctl status codeclass-ai-agent --no-pager"

uninstall-linux:
	ssh $(DEPLOY_HOST) " \
		systemctl stop codeclass-ai-agent && \
		systemctl disable codeclass-ai-agent && \
		rm -f /etc/systemd/system/codeclass-ai-agent.service && \
		systemctl daemon-reload"
