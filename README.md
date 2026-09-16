<p align="center">
  <img src="servemedia/web/static/logo.svg" alt="ServeMedia" width="200">
</p>

<h1 align="center">ServeMedia</h1>

The Self-hosted Media-Server for Linux: scan your files, fetch metadata, and play in the browser. On start it opens in the browser and listens locally (default port **7676**).

## Requirements

- Linux x86_64
- `curl` and `python3` (for the installer)
- A real terminal (the installer shows a menu)

GPU transcode uses host Mesa/`libva` when available; software encode is the fallback.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/AlyShmahell/servemedia/main/install.sh | bash
```

Pick a GitHub release. The archive includes MatchMedia and vendor libraries (htmx, video.js, hls.js, ffmpeg). The installer writes `~/.servemedia`, links `~/.local/bin/servemedia`, and adds a user-scope desktop entry. Add `~/.local/bin` to `PATH` if `servemedia` is not found. Override the install prefix with `SERVEMEDIA_HOME`.

Archives are also on [GitHub Releases](https://github.com/AlyShmahell/servemedia/releases): `servemedia-<ver>-linux-amd64.tar.gz`.

## Start

Run `servemedia` or the desktop entry. With a display, the browser opens automatically (`SERVEMEDIA_NO_BROWSER=1` skips that). If ServeMedia is already listening, a second start opens the app and exits.

The first visit goes to `/register` and creates the **admin**. After that, `/register` is gone. Add other users under Settings → Users.

## Controls

Arrow keys move focus among links and buttons on Home, libraries, titles, and Settings. A gamepad D-pad or stick does the same. **A** activates the focused control; **B** goes back (closes a dialog, otherwise Back / history). The focus outline appears while using arrows or a gamepad, not for mouse-only use.

On the player, the keyboard stays with video.js (seek, volume, play). Gamepad left/right seek 10 seconds, up/down change volume, **A** plays or pauses, **B** leaves the player.

If the pad does nothing in **Firefox from Flathub**, Linux often already sees it (`/dev/input/js*`) but Firefox cannot list it: the sandbox has device access and still lacks host udev (`/run/udev`). Grant it, then fully quit Firefox and reopen ServeMedia:

```bash
flatpak override --user --filesystem=/run/udev:ro org.mozilla.firefox
```

The same Filesystem grant works in Flatseal. Other Flatpak browsers (Chromium, Chrome) hit the same udev gap — use that app’s ID instead of `org.mozilla.firefox`. Distro-packaged Firefox usually needs nothing extra.

## Media

Default library roots are `/media` and `/mnt`. Point ServeMedia at your files with `SERVEMEDIA_MEDIA_PATH` or `media.path` in `~/.servemedia/data/config.yaml` (comma-separated paths show as one folder tree). Seed config stays in `~/.servemedia/config/default.yaml`; your changes go in the overlay.

On Home, add a library and Scan (local NFO/posters, or MatchMedia when metadata is ready). Provider keys live under Settings → Integrations. Playback is in the browser. Settings → Backup does one-shot and periodic `tar.zst` of the store.

## Files

| Path | Purpose |
|------|---------|
| `~/.servemedia` | Binary, seed config, tools |
| `~/.servemedia/data` | Library database, transcode cache, backups, overlay config |
| `~/.local/bin/servemedia` | Command on `PATH` |

## Building from source

See [docs/dev/workflow.md](docs/dev/workflow.md).
