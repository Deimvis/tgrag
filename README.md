# tgrag

Local keyword search over your personal Telegram chats. Works as a standalone CLI (`tgrag ask`) and as a Claude Code skill.

## Tutorial

### 1. Install

With nix (see [Nix](#nix) for system configuration):

```
nix run github:Deimvis/tgrag -- setup
```

Or from source:

```
make install   # builds and copies bin/tgrag into ~/.local/bin
```

### 2. Setup

```
tgrag setup
```

The wizard walks through everything interactively:

1. Telegram app credentials (`api_id` / `api_hash`). tgrag talks to Telegram as a regular client app (the same protocol Telegram Desktop uses), and Telegram requires every such app to have an id. Get yours once at https://my.telegram.org → "API development tools" → create an app with any name. These identify the app, not your account, and are stored in `~/.config/tgrag/config.yaml`.
2. Login: phone number, the code Telegram sends you, and your two-factor password if enabled. The resulting session file `~/.local/share/tgrag/session.json` grants full access to your account, it is `chmod 600`, never copy it anywhere.
3. Chat selection: a checklist of your chats; only the ones you tick are ever downloaded. The alternative "ALL chats" option is marked dangerous because it indexes every private conversation and group you are in (including new ones) and the people in them never agreed to that. `tgrag sync` refuses to run until a selection is saved.
4. Optional first sync.

Run `tgrag setup` again at any time to change the chat selection (login is skipped if the session is still valid).

### 3. Sync

```
tgrag sync
```

`sync` downloads the text of new messages from the selected chats into a local SQLite database (`~/.local/share/tgrag/tgrag.db`), groups them into conversation windows and updates the full-text index. It is incremental: each chat remembers the last seen message id, so repeated runs only fetch what is new. Telegram rate limits (FLOOD_WAIT) are respected automatically, a big chat may take a while the first time.

Options:

* `tgrag sync --chat <id|title|@username>` syncs one chat
* `tgrag sync --full --chat <id>` drops local data of a chat and re-downloads it (the only way to pick up edits and deletions of already stored messages)
* `tgrag sync --every 30m` keeps running and syncs on an interval (foreground; on macOS prefer the launchd agent from the nix module below)

### 4. Search

```
tgrag chats lesha
tgrag search "квартир*" --chat 111 --after 2024-03-01
tgrag context --chat 111 --message 42
tgrag thread --chat 111 --message 42
tgrag stats
```

All commands print JSON. Query syntax: terms are ANDed, `OR` / `NOT`, trailing `*` for prefix (covers Russian word forms), `"exact phrase"`.

### 5. Ask

* Claude Code skill (no API key): install the skill (`make install-skill` or `services.tgrag.skill.enable` in nix) and ask Claude Code anything about your chats. To avoid permission prompts allow `Bash(tgrag chats:*)`, `Bash(tgrag search:*)`, `Bash(tgrag context:*)`, `Bash(tgrag thread:*)`, `Bash(tgrag stats:*)` in Claude Code settings; keep `tgrag sync` prompting so network activity stays explicit.
* Standalone: `ANTHROPIC_API_KEY=... tgrag ask "what did Lesha say about the apartment in spring?"` runs the same tool loop with the Claude API.

## Nix

The flake exposes `packages.<system>.tgrag`, `overlays.default` and a home-manager module `homeManagerModules.default` with:

```nix
services.tgrag = {
  enable = true;
  autoSync = {
    enable = true;        # launchd agent running `tgrag sync` periodically
    intervalMinutes = 30;
  };
  skill.enable = true;    # installs ~/.claude/skills/telegram-search
};
```

In a nix-darwin + home-manager system flake:

```nix
inputs.tgrag.url = "github:Deimvis/tgrag";
# ...
home-manager.users.<name>.imports = [ inputs.tgrag.homeManagerModules.default ];
home-manager.users.<name>.services.tgrag = { enable = true; autoSync.enable = true; };
```

Notes:

* `tgrag setup` is interactive and must be run manually once after the rebuild; the launchd agent logs to `~/Library/Logs/tgrag-sync.log` and stays idle (exits with the "chat selection is not configured" error) until setup is done.
* If `~/.claude/skills` is already managed as a whole directory (e.g. linked from your system flake), leave `skill.enable = false` and copy `skill/telegram-search` into that directory instead.
* `nix flake check` runs unit tests; `make test-nix` evaluates the module test in `nix/tests.nix`.

## Config

`~/.config/tgrag/config.yaml`:

```yaml
telegram: {api_id: 12345, api_hash: "..."}
storage: {data_directory_path: /Users/me/.local/share/tgrag}
sync:
  chat_selection: {mode: whitelist, chat_ids: [111, 222]}   # or mode: all
  max_messages_per_chat: 0        # 0 = unlimited
  is_skipping_channels: true      # broadcast channels
  is_skipping_bots: true
windows: {gap_minutes: 20, max_chars: 2500}   # conversation window splitting, run `tgrag reindex` after changing
ask: {model: claude-opus-5-5, max_iterations: 20}
```

## Limits

* Text only, media and voice messages are skipped
* Keyword search only (FTS5/BM25), no embeddings yet
