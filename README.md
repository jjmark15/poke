# poke

Unix-only process supervisor that restarts a child command when you HTTP-poke it.

```bash
poke -- npm run dev
curl -X POST http://127.0.0.1:9999/poke
```

## Usage

```
poke [flags] -- <command> [args...]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `127.0.0.1:9999` | HTTP listen address |
| `-timeout` | `5s` | Graceful kill wait before SIGKILL |

## HTTP

- `POST /poke` — kill (TERM → timeout → KILL process group) and start the command again (blocking)
- `GET /health` — liveness (`ok`)

## Notes

- Direct exec only (no shell). For pipes/`&&`, wrap: `poke -- sh -c 'go build -o /tmp/app && /tmp/app'`
- No file watching; something else must call `/poke`
- No auto-restart on crash; poke again to start
- Child inherits cwd/env; stdin is `/dev/null`; stdout/stderr pass through
