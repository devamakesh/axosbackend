# AXOS Backend (`axosbackend`)

Unified Go backend and CLI scripts for **Personal OS (ALIEN X & AI Coach)**. Built with the Go standard library (`net/http`) with zero external dependencies.

## Features

- **Unified HTTP API (`0.0.0.0:8787` by default)**:
  - `POST /v1/status` — Authenticated service readiness and configured provider check
  - `POST /v1/chat` — ALIEN X personal, AX-CEO, and AX-Hacker multi-provider chat (`auto`, `gemini`, `deepseek`, `sarvam`) with automatic failover and `<think>` tag stripping
  - `POST /v1/transcribe` — Tamil & English speech-to-text via Sarvam (`saaras:v4`)
  - `POST /v1/speak` — Tamil & English text-to-speech via Sarvam (`bulbul:v3`)
  - `POST /coach/chat` — AI Coach conversation endpoint grounded in daily vault snapshots
  - `GET /health` — Health check endpoint
- **CLI Scripts**:
  - `cmd/setup` (`axosbackend setup`) — Generates a 64-character hex `ALIENX_SERVER_TOKEN` in `.env` with `0600` permissions
  - `cmd/smoke` (`axosbackend smoke [--diagnose|--models]`) — Synthetic Tamil/English provider smoke tests and upstream model diagnostics
  - `cmd/server` (`axosbackend server`) — Runs the personal ALIEN X & Coach HTTP server

## Usage

From the repository root:

```bash
# Generate ALIENX_SERVER_TOKEN in .env
npm run alienx:setup

# Start the Go server (default http://0.0.0.0:8787)
npm run alienx:server

# Run provider smoke checks
npm run alienx:smoke

# Run Go test suite
npm run backend:test
```

Or directly with `go` inside `axosbackend/`:

```bash
go run ./cmd/setup
go run ./cmd/server
go run ./cmd/smoke --diagnose
go test -v ./...
```
## Docker

From inside `axosbackend/`, build and start the backend:

```bash
docker build -t axosbackend .
docker run --rm --name axosbackend -p 8787:8787 --env-file .env axosbackend
```

The `.env` file must contain `ALIENX_SERVER_TOKEN` (at least 32 characters)
and at least one provider key: `GEMINI_API_KEY`, `DEEPSEEK_API_KEY`, or
`SARVAM_API_KEY`. Credentials are supplied at runtime and excluded from the
build context. The container runs as a non-root user and includes CA
certificates for provider HTTPS requests.

Check readiness at `http://localhost:8787/health`. If `.env` overrides
`ALIENX_PORT` or `PORT`, adjust the container port mapping to match.
