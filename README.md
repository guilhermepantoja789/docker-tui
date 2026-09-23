# docker-tui

Terminal UI for Docker telemetry and lifecycle management. Optimized for large fleets via viewport-priority one-shot stats (no streaming stats connections).

## Features

- Live container metrics (CPU, memory, network I/O, block I/O)
- Networks, volumes, and images views
- Container lifecycle: start, stop, restart, remove
- Local and remote Docker hosts via contexts / `DOCKER_HOST`
- Scales to thousands of containers by sampling visible rows first

## Requirements

- Go 1.22+
- Access to a Docker Engine API (local socket or remote)

## Install / Run

```bash
go run ./cmd/docker-tui
```

```bash
go build -o docker-tui ./cmd/docker-tui
./docker-tui --context my-remote
./docker-tui --host tcp://192.168.1.10:2375
./docker-tui --stats-concurrency 32 --stats-interval 2s
```

## Keys

| Key | Action |
|-----|--------|
| `1`–`4` / Tab | Switch views (containers, networks, volumes, images) |
| `j`/`k` | Move selection |
| `/` | Filter |
| `s` | Cycle sort (name, state, cpu, mem) |
| `a` | Start container |
| `t` | Stop container |
| `r` | Restart container |
| `d` | Delete (confirm) |
| `H` | Host / context picker |
| `q` | Quit |

## Architecture notes

Stats use one-shot `GET /containers/{id}/stats?stream=false` with a fixed worker pool. Inventory comes from `ContainerList` plus the Events API, with periodic full reconcile. The UI publishes its viewport so only on-screen (plus buffer) containers are sampled eagerly.
