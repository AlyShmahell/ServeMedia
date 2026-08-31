<p align="center">
  <img src="medora/web/static/logo.svg" alt="Medora" width="200">
</p>

<h1 align="center">Medora</h1>

Self-hosted media library for Linux: scan your files, fetch metadata, and play in the browser. Runs as a host app (not a container) at http://127.0.0.1:7676.

## Requirements

- Linux x86_64
- `curl` and `python3` (for the installer)
- A real terminal (the installer shows a menu)

GPU transcode uses host Mesa/`libva` when available; software encode is the fallback.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/AlyShmahell/medora/main/install.sh | bash
```

Pick a GitHub release. The archive includes Matchora and vendor (htmx, video.js, hls.js, ffmpeg). The installer writes `~/.medora`, links `~/.local/bin/medora`, and adds a userscope desktop entry. Add `~/.local/bin` to `PATH` if `medora` is not found. Override the install prefix with `MEDORA_HOME`.

Archives are also on [GitHub Releases](https://github.com/AlyShmahell/medora/releases): `medora-<ver>-linux-amd64.tar.gz`.

## Start

Run `medora` or the desktop entry. With a display, the browser opens automatically (`MEDORA_NO_BROWSER=1` skips that). If Medora is already listening, a second start opens the URL and exits.

The first visit goes to `/register` and creates the **admin**. After that, `/register` is gone. Add other users under Settings → Users.

## Media

Default library roots are `/media` and `/mnt`. Point Medora at your files with `MEDORA_MEDIA_PATH` or `media.path` in `~/.medora/data/config.yaml` (comma-separated paths show as one folder tree). Seed config stays in `~/.medora/config/default.yaml`; your changes go in the overlay.

On Home, add a library and Scan (local NFO/posters, or Matchora when metadata is ready). Provider keys live under Settings → Integrations. Playback is in the browser. Settings → Backup does one-shot and periodic `tar.zst` of the store.

## Files

| Path | Purpose |
|------|---------|
| `~/.medora` | Binary, seed config, tools |
| `~/.medora/data` | Library database, transcode cache, backups, overlay config |
| `~/.local/bin/medora` | Command on `PATH` |

## Building from source

See [docs/dev/workflow.md](docs/dev/workflow.md).
