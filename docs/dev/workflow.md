# Medora workflow

## Prerequisites

- **Podman** with `podman compose` (or `podman-compose`)
- Do **not** install or run Go, Node, or Playwright on the host for Medora tests

Application lifecycle (optional Docker) is handled by root `./run`.  
**All automated tests are Podman-only** via `./tests/run`.

## Pull toolchain images

```bash
podman compose -f toolchains/compose.yaml --profile pull pull
```

Or: `./tests/run` → **Pull toolchains**

Images are declared only in [`toolchains/compose.yaml`](../../toolchains/compose.yaml) (Go, Playwright, Alpine). No app services.

## Run tests

```bash
./tests/run
```

Menu:

1. Pull toolchains  
2. Run unit (`go test` inside `tests/unit` image)  
3. Run smoke (Playwright against compose `medora`)  
4. Run all  

Non-interactive (CI):

```bash
./tests/run   # without a TTY runs unit then smoke
```

Equivalent compose:

```bash
podman compose -f tests/compose.yaml --project-directory tests build unit
podman compose -f tests/compose.yaml --project-directory tests run --rm unit

podman compose -f tests/compose.yaml --project-directory tests up -d --build medora
podman compose -f tests/compose.yaml --project-directory tests run --rm smoke
```

## Hard rules

- Never `go test`, `npm test`, or `npx playwright` on the host for this repo’s automated test path  
- Never use Docker as the test runner (`./tests/run` refuses Docker-only environments)  
- App data for manual use lives in `./data/` (gitignored); test data uses a compose volume  

## App quick start (manual)

1. `cp medora/.env.example medora/.env` and set `MEDIA_PATH`  
2. `./run` → Start (Podman preferred)  
3. Open http://127.0.0.1:7676 → `/register` creates the admin user  

## VAAPI transcode (AMD / Intel via Mesa)

Runtime image: **Alpine 3.24** (Mesa 26.x, ffmpeg 8.x). Compose mounts `/dev/dri`.

Medora probes render nodes and prefers **GPU decode + VAAPI H.264 encode**:

| Pipeline | When |
|----------|------|
| `vaapi_full` | First attempt — input `-hwaccel vaapi` + `scale_vaapi` (8-bit and 10-bit) |
| `vaapi_full_10bit` | 10-bit fallback — GPU decode, CPU 10→8 via hwdownload, GPU encode |
| `vaapi_hybrid` | CPU decode + GPU encode |
| `software` | Last resort — `libx264` |

On Mesa 26, 10-bit anime may stay on `vaapi_full` (no CPU pixel convert). If direct GPU convert fails, logs show fallback to `vaapi_full_10bit` then `vaapi_hybrid`.

**Verify on your host** (rebuild after image bump):

```bash
podman logs medora 2>&1 | rg 'transcode pipeline|10-bit direct|transcode hwaccel|ffmpeg '
podman top medora | rg ffmpeg   # expect -hwaccel vaapi and h264_vaapi
```

Optional startup log when `/usr/share/medora/vaapi-probe.mkv` (HEVC Main 10) is present: `10-bit direct GPU convert OK`.

Settings → Server shows probe status and active pipeline when transcoding.

**AMD GPU monitors:** `gpu_busy_percent` often stays low during VAAPI encode; the video engine (VCN) may not appear in generic widgets. Trust logs and Settings → Server instead.

**Rootless Podman (AMD):** if render nodes exist but probe fails with `stat: permission denied`, add your user to `render` and `video` groups, or uncomment `group_add` in [`medora/compose.yaml`](../../medora/compose.yaml) (see [`medora/.env.example`](../../medora/.env.example)).

**Integration tests** (skip without GPU):

```bash
podman exec medora go test ./internal/transcode/ -run Vaapi -count=1
```

Set `hwaccel: none` in config to force software.
