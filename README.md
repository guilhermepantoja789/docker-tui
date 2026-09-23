# docker-tui

Terminal UI for Docker telemetry and lifecycle management. Optimized for large fleets via viewport-priority one-shot stats (no streaming stats connections).

## Features

- Live container metrics (CPU, memory, network I/O, block I/O)
- Networks, volumes, and images views
- Container lifecycle: start, stop, restart, remove
- Inspect detail (curated view + raw JSON)
- Side-by-side live logs (up to 2 follow streams)
- Exec / attach via the Docker CLI (TTY handoff)
- Local and remote Docker hosts via contexts / `DOCKER_HOST`
- Scales to thousands of containers by sampling visible rows first

## Requirements

- Go 1.26+
- Access to a Docker Engine API (local socket or remote)
- `docker` CLI on `PATH` for exec / attach

## Install / Run

```bash
go install github.com/guilhermepantoja789/docker-tui/cmd/docker-tui@latest
```

```bash
go run ./cmd/docker-tui
```

```bash
go build -o docker-tui ./cmd/docker-tui
./docker-tui --context my-remote
./docker-tui --host tcp://192.168.1.10:2375
./docker-tui --stats-concurrency 32 --stats-interval 2s
./docker-tui --log-tail 200 --log-buffer 5000
```

## Keys

| Key | Action |
|-----|--------|
| `1`–`4` / Tab | Switch views (containers, networks, volumes, images) |
| `j`/`k` | Move selection |
| `/` | Filter |
| `s` | Cycle sort (name, state, cpu, mem) |
| `enter` | Inspect selected container |
| `o` | Open / add log pane (max 2, side-by-side) |
| `e` | Exec into running container (`docker exec -it`) |
| `A` | Attach to running container (`docker attach`) |
| `a` | Start container |
| `t` | Stop container |
| `r` | Restart container |
| `d` | Delete (confirm) |
| `H` | Host / context picker |
| `q` | Quit |

### Inspect mode

| Key | Action |
|-----|--------|
| `j`/`k` | Scroll |
| `J` | Toggle curated / raw JSON |
| `o` | Open logs for this container |
| `esc` | Close |

### Logs mode

| Key | Action |
|-----|--------|
| `j`/`k` | Scroll focused pane |
| `h`/`l` | Cycle pane focus |
| `f` | Toggle follow (stick to bottom) |
| `tab` / `o` | Return focus to container list (add another pane with `o`) |
| `esc` | Close focused pane |

## Architecture notes

Stats use one-shot `GET /containers/{id}/stats?stream=false` with a fixed worker pool. Inventory comes from `ContainerList` plus the Events API, with periodic full reconcile. The UI publishes its viewport so only on-screen (plus buffer) containers are sampled eagerly.

Log follow streams are managed by a dedicated session hub (not the lifecycle action queue), demuxed with Docker's stdcopy framing, and capped per-pane by `--log-buffer`.
