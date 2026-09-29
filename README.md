# picode-docker

A [PiCode](https://github.com/cfpperche/picode) extension that shows your
Docker containers, grouped by compose project, and starts, stops and
restarts them — with an agent, from a workspace, or from the command
palette. It replaces PiCode's old built-in Docker app (ADR-0230, D3): the
same Docker Engine client, ported out to its own installable extension.

> v0.2 — plan in [docs/plan.md](docs/plan.md).

## Install

In PiCode: **Extensions → Install extension**, paste
`https://github.com/cfpperche/picode-docker`, review, **Install**, then turn
it on in the workspaces where you want it, or let it turn itself on: any
workspace whose folder has a `docker-compose.yml`, `compose.yml` or
`Dockerfile` does that on its own (you can always turn it back off).

## In PiCode

- **Two agent tools** (`docker_containers`, `docker_container`) let any
  agent read what is running and one container's state, resources and
  logs — no confirmation needed, since they only read. Deeper tools
  (start/stop/restart, history, resource cleanup, health monitoring) stay
  in PiCode's `packages/pi-sysadmin`, which keeps its own audited backend;
  see the [Docker guide](https://github.com/cfpperche/picode/blob/main/docs-site/guide/docker.md).
- **The Docker page** (Apps → Docker, or its tab) lists containers by
  compose project, and a container's detail: state, health, a resource
  sample, recent logs, and Start / Stop / Restart.
- **Open with**: `docker-compose.yml`, `compose.yml` and `Dockerfile` in
  PiCode's file view offer **Open in Docker**.
- **Menu and palette**: the workspace menu and the command palette have
  *Open Docker*.
- **Turns itself on** in a workspace with a compose file or a Dockerfile,
  unless you turned it off there.

## Requirements

- PiCode with extensions (ADR-0230).
- A Docker Engine reachable over its Unix socket (`DOCKER_HOST`, the
  current `docker context`, or `/var/run/docker.sock`). Never a shell, never
  a remote (`tcp://`/`ssh://`) endpoint, never elevated privileges.
- Go on the machine running PiCode (the extension's process runs as
  `go run`, so it needs no separate build step).

## Scope (v1)

Container list, detail, logs and start/stop/restart. Not yet ported from
the built-in app: the resource-history/health-monitor/maintenance-plans
layer. See [docs/plan.md](docs/plan.md).

## License

Apache-2.0.
