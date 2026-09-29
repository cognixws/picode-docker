# picode-docker — plan

## What it is

PiCode's built-in Docker app (ADR-0230, D3, E5) moved out to an installable
extension: proof that a first-party PiCode feature can live as an
extension without losing anything the owner needs day to day, and that
doing so leaves PiCode's own core more neutral.

## How it works

- `engine/` is the Docker Engine client (ported from PiCode's
  `internal/docker`): HTTP over the Engine's Unix socket, never a shell for
  container operations, never elevated privileges. `LocalClient` resolves
  the socket the same way the built-in app did: `PICODE_DOCKER_HOST`, else
  `DOCKER_HOST` (unless a Docker context is selected), else the selected
  context's Unix endpoint, else `/var/run/docker.sock`.
- `server/` is the extension's background process (`go run
  ${extensionDir}/server`, PiCode's process door): a small HTTP server on
  `PICODE_EXT_PORT`, answering only PiCode's own proxy (`X-PiCode-Proxy-Secret`).
  Routes: `GET /containers` (grouped by compose project), `GET
  /containers/{id}` (detail, a resource sample, recent logs), `POST
  /containers/{id}/action` (`start`/`stop`/`restart`, refused from a state
  where it makes no sense).
- `ui/containers.html` is the page (PiCode's `__picode/tokens.css`):
  the container list, a detail panel, and the same actions.

## Decisions

- **v1 scope.** List, detail, logs, start/stop/restart — what most owners
  use most days. The built-in app's maintenance layer (multi-step plans,
  a health-monitor loop, incidents) is not ported; it can be a later
  version if it turns out to matter away from core.
- **No agent-facing MCP tool.** The built-in app was owner-facing (buttons
  in a view, not an agent tool), so v1 keeps that shape: no
  `agent.mcpServers`. An agent that needs to act on a container today would
  do it from a terminal, as before.
- **`go run`, not a shipped binary.** No per-platform build to maintain
  and nothing to keep in sync with PiCode's own Go version; the tradeoff is
  needing Go on the machine running PiCode, which already builds PiCode
  itself from source in the owner's setup.
- **Existing operations history in PiCode's core database is left alone.**
  Moving this app out does not delete `docker_operations`/`docker_jobs`/
  `docker_monitors`/`docker_incidents` or their migrations in PiCode's own
  store; PiCode's core change (removing the built-in app) simply stops
  writing to them. Dropping that history is a separate, deliberate choice
  for later, not a side effect of this move.

## Roadmap

1. v0.1: engine client ported with its tests, the background process, the
   containers page, Open with, menu/palette commands, activation by
   compose/Dockerfile. Live-tested against the owner's real Docker
   containers (read-only checks; no container mutated without asking).
2. The owner installs it, tries it side by side with the built-in app.
3. PiCode's core removes the built-in Docker app (ADR-0230 E5): the
   `apps.BuiltIns` registration, `internal/apps/docker*.go`,
   `internal/server/docker*.go`, `internal/docker/*`, their tests and docs
   mentions.
4. Later, maybe: the maintenance layer (plans, health monitor, incidents),
   an agent-facing tool, TCP/SSH Docker endpoints.
