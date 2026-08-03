# Roadmap: Whisper subtitle generation (shelved)

Implemented once, then removed from the product to keep the container image small and drop an unreliable GPU build path. This note is enough to recreate the feature.

## Why it was shelved

- Image grew to ~1.2 GB mostly from **Mesa Vulkan ICDs** (`mesa-vulkan-ati` / `intel` / `swrast`) plus **whisper.cpp Vulkan** shared libs — not from `ggml-tiny-q5_1` (~31 MB).
- Alpine musl + Vulkan `whisper-cli` needed edge `glslang`/`shaderc` (3.22 packages broke `get_rows_iq1_m` shader link).
- Latency is minutes per episode; UX cost vs value was not worth shipping yet.

## Goal / flow

```
Subs modal (languages + model dropdown, GPU status read-only)
  → async_jobs kind=subtitles
  → ffmpeg extract mono 16 kHz WAV
  → one-shot whisper-cli -m model -l lang -osrt
  → write sidecar next to video
  → process exits (model ejected from GPU/RAM)
```

- GPU: auto when `/dev/dri/renderD*` exists (`-ng` otherwise). Not tied to model choice.
- Models: bundled `tiny-q5_1` + user downloads under `/data/whisper` from Hugging Face `ggerganov/whisper.cpp` (`ggml-*.bin` catalog at download time).

## Package layout (recreate)

| Area | Role |
|------|------|
| `internal/whisper` | `StoreResolver` (store then bundled), `ListHFCatalog` / `DownloadModel`, `Runner.Transcribe`, `HasGPU` / `GPUStatusLabel` |
| `internal/fetch` | `SubtitlesPayload{languages, model}`, `runSubtitles` / `generateOneSub` |
| `internal/server` | HTMX modal + fetch; Settings → Whisper admin page |
| Config | `whisper.models_dir`, `store_dir`, `default_model`, `bin` |

### Sidecar naming

- File: `{videoBase}.{lang}.whisper-{modelId}.srt`
- Player title: `AI whisper-{modelId} ({lang})`
- Discovery regex already in `media/sidecar.go` (keep for leftover files)

Helpers: `metadata.WhisperSidecarName` / `WhisperSidecarPath`.

### UI

- Card **Subs** → modal: model `<select>` of locally available models, language checkboxes, `GPU: on/off` text.
- **Settings → Whisper** (admin): list local models (bundled non-deletable), HF catalog download, delete store-only models.

### Routes (were)

- `GET/POST /hx/fetch/subtitles`
- `GET /settings/whisper`
- `POST /hx/whisper/download`, `DELETE /hx/whisper/{id}`

## Container (recreate carefully)

1. **Stage order:** `whisper-build` first (no app `COPY` → cache), then Go build, then Alpine final.
2. **Pin** whisper.cpp: `ARG WHISPER_CPP_REF=v1.9.1` + `git clone --branch`.
3. **Alpine final** with musl: build `GGML_VULKAN=1` on Alpine; do not copy glibc `ghcr.io/ggml-org/whisper.cpp:main-vulkan`.
4. **edge** `glslang` / `shaderc` on the whisper-build stage (fixes missing `get_rows_iq1_m_*` symbols with stock 3.22 toolchain).
5. Bake only `ggml-tiny-q5_1.bin` under `/usr/share/medora/whisper/`. Larger models → `/data/whisper` downloads only.
6. Runtime: `ffmpeg`, VAAPI (`mesa-va-gallium`), plus Vulkan ICDs **only if** GPU whisper is required (they dominate image size).
7. Entrypoint: `mkdir -p /data/whisper`. Under **rootless Podman**, run Medora as container root (maps to host user) so `/media` sidecar writes work — do **not** `su-exec` to UID 1000.

## Known bug: SRT body annotation

Do **not** prepend free text like `AI generated whisper-<id>` to the SRT body. ffmpeg’s SRT demuxer then fails (`Invalid data found`) when converting to WebVTT, so `/play/.../sub/sc-….vtt` returns 500: track appears in the UI, no cues on video.

- Mark AI origin via **filename** only.
- If old annotated files exist: strip the preamble in `EnsureSidecarVTT` before ffmpeg, or regenerate/delete those sidecars.

## Image size guidance

| Component | Impact |
|-----------|--------|
| Mesa Vulkan ICDs | Dominant (hundreds of MB) |
| libggml-vulkan + whisper-cli | Large |
| `ggml-tiny-q5_1` | ~31 MB |
| Prefer store downloads for turbo/large | Keeps image lean |

## Out of scope when re-adding

- Remote subtitle providers (OpenSubtitles / Gestdown / etc.) — abandoned earlier; Whisper was the replacement.
- Persistent whisper-server — use one-shot `whisper-cli` and wait/reap so the model is ejected after each job.
