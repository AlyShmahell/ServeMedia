# Medora workflow

## Prerequisites

- **Podman** with `podman-compose` (or `podman compose`)
- Do **not** install or run Go, Node, or Playwright on the host for Medora tests

Application lifecycle is `./build/run` (host binary). **All automated tests are Podman-only** via `./tests/run`.

## Dist

[`build/Containerfile`](../../build/Containerfile) is a one-shot **builder**, not a runtime. Host driver is [`./build/run`](../../build/run) (TTY menu, `podman-compose up --build`). Image helper is [`build/build`](../../build/build) (`vendor` at image build, `stage` at container start). Go stage: `docker.io/library/golang:1.26-bookworm`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. The image copies the binary, `share/config` → `config/`, first-party static → `public/`, `LICENSE`, `VERSION`, unpacks Matchora v0.0.3 into `tools/matchora/`, and curls third-party JS into `vendor/`. Compose bind-mounts `build/dist` (`:z`) and `build/cache` (`:z`). At container start, **`build/dist` is wiped first** (including `data/`; `.gitkeep` kept). ffmpeg (static x264, dynamic VAAPI) is copied from `build/cache/ffmpeg` when `ffmpeg`, `ffprobe`, and `LICENSE` are present; otherwise it is compiled into the cache, then copied to dist. The container writes `/dist` to `/out` and exits. `build/cache` is never deleted. Delete `build/cache/ffmpeg` to force a recompile after bumping `ffmpeg_src_url` / `x264_src_url`.

```bash
./build/run
```

Choose **run**, **(re)build & run**, **(re)build & prepare**, or **(re)build & package** (arrow keys, Enter). **run** execs the current `build/dist/medora` (error if missing). Prepare verifies `tools/matchora/matchora`, fetches third-party `vendor/` if missing, runs Matchora `--prepare` (llama.cpp under `tools/matchora/vendor/llama.cpp`), then exits. Package writes two archives with root `medora/` and Medora’s `LICENSE`: slim `medora-<ver>-linux-amd64.tar.gz` (no `{exeDir}/vendor/`, slim Matchora) and `medora-<ver>-linux-amd64-bundled.tar.gz` (vendor + llama.cpp after `--prepare`, with fetched licenses).

The binary defaults to `{exeDir}/config/default.yaml`. Writable data: `{exeDir}/data` (store, transcode, backups, optional `config.yaml` overlay). Media root: `MEDORA_MEDIA_PATH` or overlay `media.path` (comma-separated paths share one picker tree). ffmpeg is `{exeDir}/vendor/ffmpeg` (`MEDORA_FFMPEG` override).

## Run tests

```bash
./tests/run
```

Menu:

1. Run unit (`go test` inside `tests/unit` image)
2. Run smoke (Playwright against compose `medora` on the dist tree)
3. Run all

Non-interactive (CI):

```bash
./tests/run   # without a TTY runs unit then smoke
```

`./tests/run` builds `build/dist/` (same builder as `./build/run`) before smoke, then runs the existing unit and smoke compose services. It does not use Matchora’s check / live / stub-chat harness. Smoke bind-mounts `tests/fixtures/media` **read-only** so persist-beside-media cannot dirty the fixtures. Matchora’s overlay (`tests/matchora-overlay.yaml`) points OMDb/TVMaze/Jikan/TMDB at `omdb-stub` so synthetic show names stay unmatched; Film Title still hits the OMDb search fixture.

## Hard rules

- Never `go test`, `npm test`, or `npx playwright` on the host for this repo’s automated test path
- Never use Docker as the test runner (`./tests/run` refuses Docker-only environments)
- App data for manual use lives in `{exeDir}/data` (gitignored); test data uses a compose volume

## App quick start (manual)

1. `./build/run` → **(re)build & run** (or **run** if `build/dist/medora` already exists)
2. Open http://127.0.0.1:7676 → `/register` creates the admin user
3. Set `MEDORA_MEDIA_PATH` to your library root(s) if they are not already in the overlay (comma-separated)

## Process stats

Idle `medora` uses ~0% CPU, so default `top` (sorted by CPU) hides it. `./build/run` `exec`s `build/dist/medora`; Ctrl+C stops it. Matchora is a second process (`matchora` on `127.0.0.1:7680`).

```bash
pgrep -a medora
top -p "$(pgrep -d, -f '/medora$')"
ps -o pid,pcpu,rss,comm -p "$(pgrep -n -f '/medora$')"
```

## VAAPI transcode (AMD / Intel via Mesa)

GPU encode uses **host** Mesa/libva and `/dev/dri` (same idea as Matchora’s Vulkan note). Dist ffmpeg is built in the Podman builder from pinned FFmpeg source and stored in `build/cache/ffmpeg` so later rebuilds skip compile: **libx264 is statically linked**; **libva/libdrm stay dynamic** so the published `linux-amd64` tarball can use the host GPU. Fully static builds (for example BtbN) cannot drive host VAAPI. Software `libx264` remains the probe fallback.

Runtime on the target: `libva.so.2`, `libdrm.so.2`, and Mesa DRI/VA (Fedora: `libva libdrm mesa-dri-drivers mesa-va-drivers`; Debian: `libva2 libdrm2 mesa-va-drivers`). Set `hwaccel: none` in `{exeDir}/data/config.yaml` to skip the probe.

Medora probes render nodes and prefers **GPU decode + VAAPI H.264 encode**:

| Pipeline | When |
|----------|------|
| `vaapi_full` | First attempt — input `-hwaccel vaapi` + `scale_vaapi` (8-bit and 10-bit) |
| `vaapi_full_10bit` | 10-bit fallback — GPU decode, CPU 10→8 via hwdownload, GPU encode |
| `vaapi_hybrid` | CPU decode + GPU encode |
| `software` | Last resort — `libx264` |

Settings → Server shows probe status and active pipeline when transcoding. Set `hwaccel: none` in the overlay to force software.
