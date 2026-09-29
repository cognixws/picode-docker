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
- `mcp/` is the extension's agent tools (`agent.mcpServers`, `go run
  ${extensionDir}/mcp`, MCP over stdio): `docker_containers`,
  `docker_container`. Talks to the Engine socket directly through the same
  `engine` package `server/` uses — never through PiCode's HTTP relay, since
  an MCP server is a subprocess with the same OS access any local program
  has (ADR-0230). `engine/detail.go` holds the `Row`/`Detail` shapes and the
  connect/validate helpers both `server/` and `mcp/` share.

## Decisions

- **v1 scope.** List, detail, logs, start/stop/restart — what most owners
  use most days. The built-in app's maintenance layer (multi-step plans,
  a health-monitor loop, incidents) is not ported; it can be a later
  version if it turns out to matter away from core.
- **Only read-only tools migrated from pi-sysadmin, not all of them
  (2026-09-29).** `docker_containers`/`docker_container` moved here because
  they are stateless passthroughs to the Engine socket — nothing this
  extension does not already have. `docker_manage`/`docker_history` stayed
  on `packages/pi-sysadmin`, calling PiCode's `/api/docker/*`: they depend
  on PiCode's own audited store (idempotency keys, per-container locks,
  durable history) that this extension has no equivalent of, and building
  one is a real, separate project, not a rename. The rest of pi-sysadmin's
  tools (plans, jobs, health, diagnosis, monitors) depend even more on
  that store, plus a background collector goroutine tied to PiCode's own
  daemon lifecycle — sizing that migration is in the picode repo's
  docs/handoff/open/extensions.md, owner's call whether to do it.
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
   containers (read-only checks; no container mutated without asking). Done.
2. The owner installed it, tried it side by side with the built-in app. Done.
3. PiCode's core removed the built-in Docker app (ADR-0230 E5). Done,
   2026-09-29.
4. v0.2: `docker_containers`/`docker_container` as agent tools
   (`agent.mcpServers`), removed from `packages/pi-sysadmin` in the same
   move. Live-tested (34 real containers, one real detail+logs read).
   Done, 2026-09-29.
5. Later, maybe: the maintenance layer (plans, health monitor, incidents)
   and its deeper agent tools, TCP/SSH Docker endpoints.
