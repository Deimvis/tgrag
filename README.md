# tgrag

Local keyword search over your personal Telegram chats, usable as an agent skill (Claude Code, Codex) or as a standalone CLI.

## Tutorial

1. Install: `nix run github:Deimvis/tgrag -- setup`, or `git clone https://github.com/Deimvis/tgrag && cd tgrag && make install` (puts `tgrag` into `~/.local/bin`).
2. Setup: `tgrag setup`. It asks for Telegram app credentials (`api_id`/`api_hash` from https://my.telegram.org → API development tools; they identify the app, not you), logs you in (phone, code from the "Telegram" service chat, 2FA), lets you tick which chats to index (or the dangerous "ALL chats" option) and runs the first sync.
3. Sync: `tgrag sync` fetches new messages of the selected chats into `~/.local/share/tgrag/tgrag.db`; run it periodically (`tgrag sync --every 4h`, or the launchd agent from the nix module). Nothing leaves your machine.
4. Install the skill and ask your agent about your chats:
    * Claude Code: `make install-skill` (copies `skill/tgrag` to `~/.claude/skills/tgrag`)
    * Codex: `make install-skill-codex` (same folder to `~/.codex/skills/tgrag`; Codex uses the same SKILL.md format)
    * any other agent: paste `skill/tgrag/SKILL.md` into its instructions file (`AGENTS.md`, system prompt); the skill only runs `tgrag` shell commands
    * no agent at all: `ANTHROPIC_API_KEY=... tgrag ask "what did Lesha say about the apartment in spring?"`

Allow `tgrag chats/search/context/thread/stats` in your agent's permissions to avoid prompts; keep `tgrag sync` prompting so network access stays explicit.

## Commands

All commands print JSON.

* `tgrag chats [query]`, `tgrag search "<query>" [--chat <id>] [--sender <name>] [--after YYYY-MM-DD] [--before YYYY-MM-DD]`, `tgrag context --chat <id> --message <id>`, `tgrag thread --chat <id> --message <id>`, `tgrag stats`
* query syntax: terms are ANDed, `OR` / `NOT`, trailing `*` for prefix (covers Russian word forms), `"exact phrase"`
* `tgrag sync --chat <id|title|@username>` syncs one chat, `--full` re-downloads it (picks up edits and deletions), `--every 4h` keeps syncing
* `tgrag import result.json` indexes a Telegram Desktop JSON export without any login
* `tgrag reindex` rebuilds the index after changing `windows` settings in config

## Config

`~/.config/tgrag/config.yaml`, written by `tgrag setup`:

```yaml
telegram: {api_id: 12345, api_hash: "...", api_hash_file_path: ""}   # exactly one of api_hash / api_hash_file_path
storage: {data_directory_path: ~/.local/share/tgrag}
sync:
  chat_selection: {mode: whitelist, chat_ids: [111, 222]}   # or mode: all
  max_messages_per_chat: 0        # 0 = unlimited
  is_skipping_channels: true      # broadcast channels
  is_skipping_bots: true
windows: {gap_minutes: 20, max_chars: 2500}
ask: {model: claude-opus-5-5, max_iterations: 20}
```

`~/.local/share/tgrag/session.json` grants full access to your Telegram account: it is `chmod 600`, never copy or commit it.

## Nix

The flake exposes `packages.<system>.tgrag`, `overlays.default` and a home-manager module:

```nix
inputs.tgrag.url = "github:Deimvis/tgrag";
home-manager.users.<name>.imports = [ inputs.tgrag.homeManagerModules.default ];
home-manager.users.<name>.services.tgrag = {
  enable = true;
  autoSync = { enable = true; intervalMinutes = 240; };   # launchd agent running `tgrag sync`
  skill.enable = true;                                    # installs the skill for claude code and codex
};
```

`tgrag setup` is still run manually once; the agent logs to `~/Library/Logs/tgrag-sync.log` and stays idle until then. Keep `api_hash` out of the repo with `api_hash_file_path` pointing at an agenix/sops secret. `nix flake check` runs unit tests, `make test-nix` evaluates the module test.

## Limits

* Text only, media and voice messages are skipped
* Keyword search only (FTS5/BM25), no embeddings yet
