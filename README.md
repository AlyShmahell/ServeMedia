# Medora

Minimal self-hosted media server (Go + HTMX + SQLite + FFmpeg). Rootless-friendly storage under `./data/store/`, app-owned `tar.zst` backups.

## Quick start

```bash
cp medora/.env.example medora/.env
# set MEDIA_PATH=/path/to/your/media
./run          # Start (Podman default; Docker optional)
```

Open http://127.0.0.1:7676 — first visit creates the **admin** via `/register`.

## Layout

| Path | Purpose |
|---|---|
| `run` | Host TUI: Start/Stop/Logs/Engine |
| `medora/` | App sources, Containerfile, compose |
| `data/` | Runtime store, transcode cache, backups (gitignored) |
| `tests/` | Podman-only unit + Playwright smoke |
| `toolchains/` | Pull-only toolchain images |
| `docs/dev/workflow.md` | Test and run workflow |

## Auth

- No users → redirect to `/register` (bootstrap **admin** only)  
- Afterward `/register` returns 404  
- Admin → Settings → Users creates ordinary users  

## Backup

In the web UI (Settings → Backup): one-shot and periodic `tar.zst` of `store/`. Restore from the same page. Not handled by `./run`.

## Tests

See [docs/dev/workflow.md](docs/dev/workflow.md). **Podman only** — no host Go/Node/Playwright.
