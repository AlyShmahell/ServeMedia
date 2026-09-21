<p align="center">
  <img src="servemedia/web/static/logo.svg" alt="ServeMedia" width="200">
</p>

<h1 align="center">ServeMedia</h1>

<p align="justified">
**ServeMedia** is `The Self-Hosted Media-Server for Linux`. It is built around a core application called MatchMedia which acts as a statistical engine responsible for directory hierarchy discovery of media libraries, title extraction and grouping, metadata search-and-fetch from providers like TVMaze, statistically scoring and ranking candidates and mapping them to the appropriate files. **ServeMedia** is built to be lightweight, minimal and concise, yet feature-complete for a self hosted media server. **ServeMedia** provides a desktop entry which starts its services then opens the browser pointed to its webpage (port **7676**), which is server-side-rendered and provides a home theater experience, complete with dri accelerated hls streamed video playback.
</p>

## Requirements

- Linux x86_64
- `curl` and `python3` (for the installer)
- A real terminal (the installer shows a menu)

GPU transcode uses host Mesa/`libva` when available; software encode is the fallback.

## Install
- from [GitHub Releases](https://github.com/AlyShmahell/servemedia/releases), using the automated installer:
```bash
curl -fsSL https://raw.githubusercontent.com/AlyShmahell/servemedia/main/install.sh | bash
```
The installer writes `~/.servemedia`, links `~/.local/bin/servemedia`, and adds a user-scope desktop entry. Add `~/.local/bin` to `PATH` if `servemedia` is not found. Override the install prefix with **env var** `SERVEMEDIA_HOME`.
## Start

Run `servemedia` or the desktop entry. With a display, the browser opens automatically (`SERVEMEDIA_NO_BROWSER=1` skips that). If ServeMedia is already listening, a second start opens the app and exits.

The first visit goes to `/register` and creates the **admin**. After that, `/register` is gone. Add other users under Settings → Users.

## How to

Start ServeMedia, create the admin, add a library, open a title, and play.

<p align="center">
  <video controls width="640" src="flatpak/screenshots/screencast.webm">
    <a href="flatpak/screenshots/screencast.webm">Watch the walkthrough</a>
  </video>
</p>

Create the admin account.

<p align="center">
  <img src="flatpak/screenshots/06-setup.png" alt="Create the admin account" width="640">
</p>

Home with libraries and recently added titles.

<p align="center">
  <img src="flatpak/screenshots/01-home.png" alt="Home with libraries and recently added titles" width="640">
</p>

Anime library poster grid.

<p align="center">
  <img src="flatpak/screenshots/02-library-anime.png" alt="Anime library poster grid" width="640">
</p>

Title page with plot and seasons.

<p align="center">
  <img src="flatpak/screenshots/03-title.png" alt="Title page with plot and seasons" width="640">
</p>

Season page with episode stills.

<p align="center">
  <img src="flatpak/screenshots/04-season.png" alt="Season page with episode stills" width="640">
</p>

Browser playback.

<p align="center">
  <img src="flatpak/screenshots/05-player.png" alt="Browser playback" width="640">
</p>

## Controls

Arrow keys or a gamepad move focus on Home, libraries, titles, and Settings. The focus outline appears while using arrows or a pad, not for mouse-only use.

<p align="center">
  <img src="servemedia/share/assets/controls-gamepad.svg" alt="Gamepad: D-pad and left stick move focus, or seek 10 seconds and change volume on the player. A activates or plays and pauses. B goes back or leaves the player." width="640">
</p>
<p align="center">
  <img src="servemedia/share/assets/controls-keyboard.svg" alt="Keyboard: arrow keys move focus, or seek and change volume on the player. Enter activates. Space plays and pauses on the player." width="640">
</p>

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
