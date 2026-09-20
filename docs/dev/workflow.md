# ServeMedia workflow

## Prerequisites

- **Podman** with `podman-compose` (or `podman compose`)
- Do **not** install or run Go, Node, or Playwright on the host for ServeMedia tests

Application lifecycle is `./build/run` (host binary). **All automated tests are Podman-only** via `./tests/run`.

## Dist

[`build/Containerfile`](../../build/Containerfile) is a one-shot **builder**, not a runtime. Host driver is [`./build/run`](../../build/run) (TTY menu, `podman-compose up --build`). Image helper is [`build/build`](../../build/build) (`vendor` at image build, `stage` at container start). Go stage: `docker.io/library/golang:1.26-bookworm`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. The image copies the binary, `share/config` → `config/`, first-party static → `public/`, `LICENSE`, and curls third-party JS into `vendor/` at image build. MatchMedia is installed at **stage** (cache is mounted then): `build/cache/matchmedia-<matchmedia.version>-linux-amd64.tar.gz` when the filename and the archive’s inner `version:` both match the pin, otherwise `matchmedia.url` (basename must be that same archive name). Compose bind-mounts `build/dist` (`:z`) and `build/cache` (`:z`). At container start, **`build/dist` is wiped first** (including `data/`; `.gitkeep` kept). ffmpeg (static x264, dynamic VAAPI) is copied from `build/cache/ffmpeg` when `ffmpeg`, `ffprobe`, and `LICENSE` are present; otherwise it is compiled into the cache, then copied to dist. The container writes `/dist` to `/out` and exits. `build/cache` is never deleted. Delete `build/cache/ffmpeg` to force a recompile after bumping `ffmpeg_src_url` / `x264_src_url`. The app version is `version` in `servemedia/share/config/default.yaml` only. The MatchMedia pin is `matchmedia.version` in the same file.

```bash
./build/run
```

Choose **run**, **(re)build & run**, **(re)build & prepare**, or **(re)build & package** (arrow keys, Enter). **run** execs the current `build/dist/servemedia` (error if missing). Prepare verifies `tools/matchmedia/matchmedia`, fetches third-party `vendor/` if missing, then exits (local dist only, not an install step). Package rebuilds dist and writes the host tarball with root `servemedia/` and ServeMedia’s `LICENSE` (`servemedia-<ver>-linux-amd64.tar.gz`: `tools/matchmedia/` plus `vendor/` with htmx, video.js, hls.js, ffmpeg; version from `config/default.yaml`). It does not build a Flatpak.

Standalone `.flatpak` is [`./flatpak/build`](../../flatpak/build) (TTY arrow menu; `flatpak-builder` runs inside the Fedora image from [`flatpak/containerfile`](../../flatpak/containerfile)). Choose **build**, **build offline**, **lint**, **regen**, or **sync**. **build** compiles the ServeMedia and MatchMedia git tags in [`flatpak/eu.alyshmahell.ServeMedia.yml`](../../flatpak/eu.alyshmahell.ServeMedia.yml) and fetches missing runtimes. **build offline** uses the cached image and `build/cache/flatpak` only. **lint** runs `flatpak-builder-lint` on the manifest. **regen** rewrites Go module lists in [`flatpak/pins/`](../../flatpak/pins/). **sync** updates git tags, JS checksums, and metainfo from the latest GitHub release, then regen (does not commit). Host Flatpak tools are not required. Runtimes and SDK stay in `build/cache/flatpak` (never wiped). First **build** or **lint** pulls Freedesktop **25.08** Platform/SDK, the golang extension, and `org.flatpak.Builder` (~GB). Outputs:

| Artifact | ffmpeg |
|----------|--------|
| `servemedia-<ver>-linux-amd64.tar.gz` | vendored (`vendor/ffmpeg`, static x264 + VAAPI) |
| `servemedia-<ver>-linux-amd64.flatpak` | no `vendor/ffmpeg`; `org.freedesktop.Platform.ffmpeg-full` (25.08) |

App id is `eu.alyshmahell.ServeMedia`. Manifest: [`flatpak/eu.alyshmahell.ServeMedia.yml`](../../flatpak/eu.alyshmahell.ServeMedia.yml) (Go build, no `vendor/ffmpeg`). Go module lists live in [`flatpak/pins/`](../../flatpak/pins/). Runtime wrapper is [`flatpak/servemedia`](../../flatpak/servemedia) (`SERVEMEDIA_ROOT=/app`). Data in the sandbox is `~/.var/app/eu.alyshmahell.ServeMedia/data/servemedia/` (`$XDG_DATA_HOME/servemedia`), not next to the binary.

Sideload:

```bash
flatpak install --user build/package/servemedia-<ver>-linux-amd64.flatpak
flatpak run eu.alyshmahell.ServeMedia
```

The `.flatpak` needs the ffmpeg-full extension (installed automatically when online). The sandbox can read and write `/media`, `/mnt`, `/run/media`, `~/Videos`, and `~/Music`; libraries outside those paths need `flatpak override --filesystem=...`. Flathub **acceptance** does not need a file on [alyshmahell.eu](https://alyshmahell.eu). The **verified badge** is later: Developer Portal token in `https://alyshmahell.eu/.well-known/org.flathub.VerifiedApps.txt` (GitHub Pages: `.nojekyll` or Jekyll `include: [".well-known"]`) or a TXT record at `_flathub.alyshmahell.eu`. Do not invent a token in this repo.

The binary defaults to `{exeDir}/config/default.yaml`. Writable data: `{exeDir}/data` on a host install (store, transcode, backups, optional `config.yaml` overlay). Media root: `SERVEMEDIA_MEDIA_PATH` or overlay `media.path` (comma-separated paths share one picker tree). ffmpeg is `{exeDir}/vendor/ffmpeg` (`SERVEMEDIA_FFMPEG` override), or `ffmpeg` on `PATH` when that tree is absent (Flatpak).

## Run tests

```bash
./tests/run
```

Menu:

1. Run unit (`go test` inside `tests/unit` image)
2. Run smoke (Playwright against compose `servemedia` on the dist tree)
3. Run all

Non-interactive (CI):

```bash
./tests/run   # without a TTY runs unit then smoke
```

`./tests/run` builds `build/dist/` (same builder as `./build/run`) before smoke, then runs the existing unit and smoke compose services. It does not use MatchMedia’s check / live / stub-chat harness. Smoke bind-mounts `tests/fixtures/media` **read-only** so persist-beside-media cannot dirty the fixtures. MatchMedia’s overlay (`tests/matchmedia-overlay.yaml`) points OMDb/TVMaze/Jikan/TMDB at `omdb-stub` so synthetic show names stay unmatched; Film Title still hits the OMDb search fixture.

## Hard rules

- Never `go test`, `npm test`, or `npx playwright` on the host for this repo’s automated test path
- Never use Docker as the test runner (`./tests/run` refuses Docker-only environments)
- App data for manual use lives in `{exeDir}/data` (gitignored); test data uses a compose volume

## App quick start (manual)

1. `./build/run` → **(re)build & run** (or **run** if `build/dist/servemedia` already exists)
2. Register the admin in the browser
3. Set `SERVEMEDIA_MEDIA_PATH` to your library root(s) if they are not already in the overlay (comma-separated)

## Process stats

Idle `servemedia` uses ~0% CPU, so default `top` (sorted by CPU) hides it. `./build/run` `exec`s `build/dist/servemedia`; Ctrl+C stops it. MatchMedia is a second process (`matchmedia` on `127.0.0.1:7680`).

```bash
pgrep -a servemedia
top -p "$(pgrep -d, -f '/servemedia$')"
ps -o pid,pcpu,rss,comm -p "$(pgrep -n -f '/servemedia$')"
```

## VAAPI transcode (AMD / Intel via Mesa)

GPU encode uses **host** Mesa/libva and `/dev/dri` (same idea as MatchMedia’s Vulkan note). Dist ffmpeg is built in the Podman builder from pinned FFmpeg source and stored in `build/cache/ffmpeg` so later rebuilds skip compile: **libx264 is statically linked**; **libva/libdrm stay dynamic** so the published `linux-amd64` tarball can use the host GPU. Fully static builds (for example BtbN) cannot drive host VAAPI. Software `libx264` remains the probe fallback.

Runtime on the target: `libva.so.2`, `libdrm.so.2`, and Mesa DRI/VA (Fedora: `libva libdrm mesa-dri-drivers mesa-va-drivers`; Debian: `libva2 libdrm2 mesa-va-drivers`). Set `hwaccel: none` in `{exeDir}/data/config.yaml` to skip the probe.

ServeMedia probes render nodes and prefers **GPU decode + VAAPI H.264 encode**:

| Pipeline | When |
|----------|------|
| `vaapi_full` | First attempt — input `-hwaccel vaapi` + `scale_vaapi` (8-bit and 10-bit) |
| `vaapi_full_10bit` | 10-bit fallback — GPU decode, CPU 10→8 via hwdownload, GPU encode |
| `vaapi_hybrid` | CPU decode + GPU encode |
| `software` | Last resort — `libx264` |

Settings → Server shows probe status and active pipeline when transcoding. Set `hwaccel: none` in the overlay to force software.
