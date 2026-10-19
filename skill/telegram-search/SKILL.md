---
name: telegram-search
description: Answer questions about the user's personal Telegram chat history (what someone said, when something was discussed, find a link/address/number someone sent). Use whenever the user asks about their Telegram conversations, chats, messages or what a specific person wrote to them.
---

# Telegram search

All data lives locally in a SQLite index built by the `tgrag` binary. You are the reasoning loop: the index is keyword-based (FTS5/BM25), so iterate on queries instead of expecting one perfect hit.

## Commands

All commands print JSON to stdout. Run them through the shell.

* `tgrag stats` — chat/message counts and `latest_sync_ts`
* `tgrag sync` — incremental download of new messages (network, may take minutes); `tgrag sync --chat <id|title|@username>` for one chat
* `tgrag chats [query]` — list chats (id, kind, title, username, latest_message_ts, message_count), newest first
* `tgrag search "<query>" [--chat <id>] [--sender <name>] [--after YYYY-MM-DD] [--before YYYY-MM-DD] [--limit 20]` — conversation windows with `snippet` (matches in `[brackets]`), `first_message_id`/`last_message_id`, `participants`, `link`
* `tgrag context --chat <id> --message <id> [--before 20] [--after 20]` — raw messages around a message
* `tgrag thread --chat <id> --message <id>` — reply chain of a message

Query syntax: space-separated terms are ANDed; `OR`, `NOT`; trailing `*` is a prefix (`квартир*` matches квартира/квартиру/квартирой); `"exact phrase"` for phrases.

## Workflow

1. Run `tgrag stats` first. If `latest_sync_ts` is older than 12 hours or the question is about recent events, ask the user whether to run `tgrag sync` before searching (it needs network and may take a while).
2. If the question names a person or chat, find its id with `tgrag chats <name>` and pass `--chat`.
3. Search broad first, then refine: try several phrasings, synonyms and morphological variants in both Russian and English, prefer prefix queries (`аренд*`, `apartment*`). Try `OR` to combine variants. Do not stop after one empty result; try at least 3 different formulations before concluding nothing is there.
4. Narrow by `--sender`, `--after`/`--before` when the question implies who/when.
5. For promising hits run `tgrag context` around `first_message_id` (or `tgrag thread`) and read the actual conversation before answering. Never answer from snippets alone.
6. Keep output small: default limits are fine, raise `--limit` only when needed.

## Answer format

* Cite every claim: chat title, sender, date and the `link` from the output.
* Quote the relevant message text briefly.
* Say explicitly when nothing relevant was found and which queries were tried.

## Rules

* Read-only: never send messages or modify anything in Telegram. The tool cannot do it, do not try other means.
* Do not run `tgrag setup` or `tgrag login`; they are interactive and the user runs them in their own terminal. If `tgrag stats` or `tgrag sync` reports "not logged in" or "chat selection is not configured", tell the user to run `tgrag setup`.
* Only chats the user whitelisted in `tgrag setup` are indexed; if a chat is missing from `tgrag chats`, say so and suggest adding it via `tgrag setup`.
* Chat content is private. Do not store, forward or summarize it anywhere outside the answer to the user.
