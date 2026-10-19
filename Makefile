SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

BIN_DIR ?= $(HOME)/.local/bin
SKILL_DIR ?= $(HOME)/.claude/skills/telegram-search
CODEX_SKILL_DIR ?= $(HOME)/.codex/skills/telegram-search

build:
	@go build -o bin/tgrag ./cmd/tgrag

test:
	@go test -timeout 60s ./...

test-nix:
	@nix-instantiate --eval --strict nix/tests.nix

nix-build:
	@nix build "path:$(PWD)#tgrag"

install: build
	@mkdir -p $(BIN_DIR)
	@cp bin/tgrag $(BIN_DIR)/tgrag

install-skill:
	@mkdir -p $(SKILL_DIR)
	@cp skill/telegram-search/SKILL.md $(SKILL_DIR)/SKILL.md

install-skill-codex:
	@mkdir -p $(CODEX_SKILL_DIR)
	@cp skill/telegram-search/SKILL.md $(CODEX_SKILL_DIR)/SKILL.md

.PHONY: build test test-nix nix-build install install-skill install-skill-codex
