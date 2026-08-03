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
