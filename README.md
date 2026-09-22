# Trackstar

A lightweight, self-hosted project tracker in the spirit of Pivotal Tracker:
Icebox → Backlog → Current iteration, points, velocity, and a keyboard-friendly
board. One Go binary, one SQLite file, no other services.

| | |
|---|---|
| Runtime dependencies | none (a binary and a writable directory) |
| Idle memory (measured) | **15–27 MB RSS** (45 MB after a request burst) — see [Resource usage](#resource-usage) |
| Startup | a few milliseconds |
| Stack | Go · SQLite (WAL) · sqlc · goose · Svelte 5 · TypeScript · Vite · SortableJS · Server-Sent Events |

## Contents

- [Using it](#using-it)
- [Architecture](#architecture)
- [Development setup](#development-setup)
- [Production build](#production-build)
- [Configuration](#configuration)
- [SQLite design](#sqlite-design)
- [Future PostgreSQL migration](#future-postgresql-migration)
- [DigitalOcean / any Debian-Ubuntu server](#digitalocean--any-debianubuntu-server)
- [Proxmox LXC installation](#proxmox-lxc-installation)
- [Docker installation](#docker-installation)
- [Cloudflare Tunnel](#cloudflare-tunnel)
- [Backups and restore](#backups-and-restore)
- [Updates and deploys](#updates-and-deploys)
- [API](#api)
- [Resource usage](#resource-usage)
- [Troubleshooting](#troubleshooting)
- [Known limitations](#known-limitations)

## Using it

The first account you register becomes the administrator. Create a project and
you land on the board:

- **Icebox** – ideas. **Backlog** – prioritised work. **Current iteration** –
  what is being worked on now. **Done** (toggle) – accepted work by iteration.
- Drag rows to prioritise, or drag them between panels. Work that is in
  progress (started/finished/delivered/rejected) stays in the current iteration.
- Each row has the one button that matters next: Start → Finish → Deliver →
  Accept/Reject → Restart. Unestimated features show the point scale
  (0 1 2 3 5 8) instead of *Start*; bugs and chores may stay unestimated.
- The backlog is cut into projected iterations using the velocity: the average
  of accepted **feature** points over the last N completed iterations
  (N = 3 by default; 10 is assumed until one iteration has completed).
- Iterations are automatic: length (1–4 weeks) and start weekday are project
  settings; nothing needs to be "closed".
- Changes by teammates appear live (typically within ~200 ms) and the changed
  row flashes briefly. The dot in the top bar is green while the live stream is
  connected.
- Every story keeps a history (state changes, estimates, owners, moves,
  renames) interleaved with its comments in the drawer.
- Deleting a story moves it to the **Trash** for 30 days: undo from the toast,
  or restore from the Trash panel. Purge happens at startup, no worker.
- Your name and password live under *your name ▸ Account*. Administrators get
  *Users*: rename, promote/demote, deactivate/reactivate, reset a password.
  Administrators can also **Reopen** an accepted story (it drops out of
  velocity history).

| Key | Action |
|---|---|
| `c` | new story (in the selected story's panel; `Shift+Enter` saves and keeps the dialog open) |
| `j` / `k` (or ↓ / ↑) | move selection |
| `h` / `l` (or ← / →) | switch panel |
| `Enter` | open selected story |
| `/` | search title + description |
| `Esc` | close dialog / drawer / search / selection |
| `?` | shortcut help |

## Architecture

```
browser ── Svelte SPA (embedded in the binary via //go:embed)
   │  JSON over /api/*   ◀── Server-Sent Events: /api/projects/:id/events
   ▼
internal/api        HTTP only: routing (net/http ServeMux), middleware, JSON, status codes
   ▼
internal/{auth,project,story,velocity,user}   services: all business rules
internal/iteration                            pure date arithmetic, no I/O
   ▼
internal/database   Store interface = generated queries + InTx(); driver specifics
   ▼
internal/database/dbgen   sqlc-generated, typed queries   ◀── db/queries/*.sql
   ▼
SQLite (WAL)                                              ◀── db/migrations/sqlite/*.sql (goose, embedded)
```

```
cmd/trackstar/          main: serve | migrate | backup | check | reset-password | healthcheck | version
.github/workflows/    ci.yml (lint, generated-code check, tests, e2e, Docker probe), release.yml (tagged releases)
internal/api/         handlers, middleware (request id, logging, origin check, trusted proxies), SSE stream
internal/events/      in-process change hub: Publish(project) → every open stream of that project
internal/auth/        bcrypt passwords, server-side sessions (HMAC-hashed tokens), login rate limit
internal/config/      TRACKSTAR_* environment → validated Config
internal/database/    Open/Migrate/InTx/Backup; sqlite.go is the only driver-specific file
internal/story/       stories, workflow (states.go), ordering (position.go), comments, labels, search
internal/project/     projects and their iteration settings
internal/iteration/   Schedule → iteration N, iteration containing t
internal/velocity/    velocity + iteration history from accepted stories
db/                   migrations, queries, sqlc.yaml
web/                  Svelte app; web/embed.go embeds web/dist
scripts/ deploy/      setup-lxc.sh (prepare an LXC, system level), setup.sh (install the app), deploy, update, backup, restore; unit, env example
```

Design decisions worth knowing:

- **Iterations are computed, not stored.** Iteration 1 starts on the project's
  start weekday on or before its creation date; iteration *n* follows by
  calendar arithmetic (DST-safe) in `TRACKSTAR_TIMEZONE`. History is derived from
  each story's `accepted_at`.
- **Ordering** uses sparse integer positions (gap 65 536, midpoint insertion),
  independently per panel. A drag rewrites exactly one row; when a gap is used
  up the panel is rebalanced once inside the same transaction. Everything that
  knows about the strategy is in `internal/story/position.go` (+ the `place`
  function), so LexoRank-style keys could replace it.
- **Moves are neighbour-based** (`prev_id` / `next_id`), resolved inside a
  transaction against the real list, so a stale client cannot corrupt order.
  State and position change atomically.
- **Sessions** are random tokens in an HttpOnly, SameSite=Lax cookie; only an
  HMAC (keyed with the session secret) is stored. State-changing requests with
  a foreign `Origin` are rejected.
- **Live updates without websockets or workers.** Every successful write
  publishes a tiny "project N changed" event to an in-process hub; each open
  board holds one Server-Sent Events stream (`GET /api/projects/:id/events`)
  and refetches the story list on receipt (debounced, echoes of its own
  changes ignored, deferred while the user is mid-drag). `EventSource`
  reconnects on its own and every reconnect refetches, so nothing is missed.
  A 30 s heartbeat keeps proxies (Cloudflare: 100 s idle limit) from closing
  the stream. Focus-refresh and a 5-minute poll remain as a safety net.
  The hub is single-process by design; a multi-instance PostgreSQL deployment
  would put `LISTEN/NOTIFY` behind the same two methods.
- **Single team.** Every signed-in user sees every project (no organisations,
  roles or per-project permissions). Only deleting a project and
  `/api/system/info` are admin-only.

## Development setup

Requirements: Go ≥ 1.26, Node ≥ 20. No Docker, no database server.

```sh
git clone <this repository> trackstar && cd trackstar
make dev        # API on :3000 + Vite with hot reload on :5173
```

Open <http://localhost:5173>. Vite proxies `/api` and `/health` to the Go
server; development data lives in `./data-dev` (Docker Compose uses `./data`,
which its container owns as root — keeping them apart avoids permission
clashes).

```sh
make test       # go test ./...  +  vitest
make e2e        # headless-Chromium end-to-end tests against bin/trackstar (web/e2e/*.mjs)
make lint       # go vet, gofmt, svelte-check, shellcheck
make sqlc       # regenerate internal/database/dbgen after editing db/queries or migrations
make migrate    # apply migrations to ./data/trackstar.db without starting the server
```

Tests use temporary SQLite databases (`database.NewTestDB`) and cover
migrations, auth, story creation, state transitions, ordering, moves between
panels, position normalisation, iterations, velocity and the HTTP API. The
frontend tests cover the board logic (move requests, optimistic moves, backlog
projection), the SortableJS adapter, the live-update client and the story
row's workflow buttons. `make e2e` drives the real binary in headless
Chromium: drag/drop (rows and empty column space), keyboard, workflow buttons,
search, trash/undo, the account dialog, and — with two browsers — live sync.

CI (`.github/workflows/ci.yml`) runs all of that plus a check that
`internal/database/dbgen` matches `db/queries`, and builds the Docker image
and probes `/health`. Pushing a tag `v*` runs `release.yml`, which publishes
`trackstar-<tag>-linux-{amd64,arm64}.tar.gz` and `SHA256SUMS` as a GitHub
release — the layout `setup.sh --repo` and `update.sh` expect.

Adding a migration: create `db/migrations/sqlite/0000N_name.sql` (goose
format), run `make sqlc`. Migrations are embedded and applied at startup.

## Production build

```sh
make build      # → bin/trackstar   (frontend embedded, static, CGO disabled)
make release    # → dist/trackstar-<version>-linux-{amd64,arm64}.tar.gz + SHA256SUMS
```

A release archive contains `trackstar`, `scripts/`, `deploy/` and this README.
The installed layout is:

```
/usr/local/bin/trackstar          the application (trackstar.previous = rollback copy)
/etc/trackstar/trackstar.env        configuration (root:trackstar 0640)
/var/lib/trackstar/trackstar.db     data (+ -wal/-shm, backups/, session_secret)
/opt/trackstar/scripts/           update.sh, backup.sh, restore.sh, …
```

The binary serves the frontend on `/`, the API on `/api/*` and `GET /health`
(`{"status":"ok"}`, checks the database, no authentication).

## Configuration

Environment only; invalid values abort startup with a list of every problem.
See [`deploy/trackstar.env.example`](deploy/trackstar.env.example).

| Variable | Default | |
|---|---|---|
| `TRACKSTAR_ADDR` | `127.0.0.1:3000` | listen address |
| `TRACKSTAR_DATA_DIR` | `./data` | created if missing |
| `TRACKSTAR_DATABASE_DRIVER` | `sqlite` | `postgres` is reserved |
| `TRACKSTAR_DATABASE_URL` | `$DATA_DIR/trackstar.db` | |
| `TRACKSTAR_PUBLIC_URL` | `http://localhost:<port>/` | decides Secure cookies and the allowed `Origin` |
| `TRACKSTAR_ALLOW_REGISTRATION` | `true` | the first account can always be created |
| `TRACKSTAR_SESSION_SECRET` | generated into `$DATA_DIR/session_secret` | ≥ 32 chars |
| `TRACKSTAR_LOG_LEVEL` | `info` | `debug` also logs `/health` |
| `TRACKSTAR_TIMEZONE` | `UTC` | where iteration days begin (tzdata is embedded) |
| `TRACKSTAR_TRUSTED_PROXIES` | `127.0.0.0/8,::1/128` | whose forwarding headers are believed |

Logs are JSON lines on stdout (journald under systemd): `time`, `level`,
`method`, `path`, `status`, `duration_ms`, `request_id`, `remote_ip`.
`GET /api/system/info` (admins) reports version, Go version, database driver
and size, uptime, memory, open live streams, and two ordering health numbers:
`position_rebalances` (renormalisations since start; rare by design) and
`largest_section` (most stories in one ordering scope).

## SQLite design

- Pure-Go driver (`modernc.org/sqlite`): no cgo, static binaries, trivial
  cross-compilation.
- `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`,
  `busy_timeout=5000`, 8 MB page cache, at most 4 connections.
  Transactions begin `IMMEDIATE`, so read-modify-write transactions (moves)
  queue instead of failing on lock upgrade.
- Migrations run automatically at startup (goose, embedded SQL).
- Backups use `VACUUM INTO` (`trackstar backup <file>`), which is safe while the
  server is writing. Never copy a live `trackstar.db` without its `-wal`.

## Future PostgreSQL migration

The application layer is already database-agnostic; what it depends on is
`database.Store` (`dbgen.Querier` + `InTx`). Portability rules the schema and
queries follow:

- explicit 64-bit integer primary keys, `RETURNING` instead of last-insert-id;
- timestamps are UTC unix seconds in `BIGINT` columns (no driver-specific time
  handling); booleans are `BOOLEAN`;
- no triggers, generated columns, or SQL-side business logic — iterations,
  velocity and ordering are computed in Go;
- queries use `sqlc.arg()` / `sqlc.narg()` / `sqlc.slice()`, never `?` or `$1`;
- search is `LOWER(title || ' ' || description) LIKE …`, identical on both
  engines, and hidden behind `story.Service.List` should FTS replace it.

To add PostgreSQL:

1. `db/migrations/postgres/00001_init.sql` — the same tables with
   `BIGINT GENERATED BY DEFAULT AS IDENTITY` keys.
2. A second block in `db/sqlc.yaml` (`engine: postgresql`, same `queries`
   directory, `out: ../internal/database/pggen`, with type overrides so nullable
   columns map to `sql.NullInt64` as they do for SQLite).
3. `internal/database/postgres.go`: open via `pgx`'s `database/sql` driver, run
   goose with `DialectPostgres`, implement `Backup` with `pg_dump` or drop it.
4. Accept `postgres` in `config.Load` and `database.Open`.

Services, handlers, tests and the frontend do not change:

```
TRACKSTAR_DATABASE_DRIVER=postgres
TRACKSTAR_DATABASE_URL=postgres://trackstar:…@db/trackstar
```

Data moves by exporting each table in id order and importing it (same column
types), then resetting the identity sequences.

## DigitalOcean / any Debian-Ubuntu server

A 512 MB droplet is plenty. On your workstation:

```sh
make release
scp dist/trackstar-*-linux-amd64.tar.gz root@droplet:
```

On the server:

```sh
tar xzf trackstar-*-linux-amd64.tar.gz && cd trackstar-*-linux-amd64
sudo ./scripts/setup.sh --public-url https://track.example.com/ --port 3000
```

Or do both in one step from your checkout (first run installs, later runs
deploy): `./scripts/deploy.sh root@droplet -- --public-url https://track.example.com/`

`setup.sh` validates the distribution, installs `ca-certificates curl tar`,
creates the `trackstar` user, `/etc/trackstar`, `/var/lib/trackstar`, installs the
binary, writes `trackstar.env` (generating a session secret), installs and
enables the hardened systemd unit, starts it and waits for `/health`. It is
idempotent: re-running keeps configuration, secret and data, and only changes
options you pass explicitly. Other sources for the binary: `--binary PATH`,
`--release-url URL`, `--repo OWNER/NAME [--version TAG]` (GitHub releases; the
repo is remembered for `update.sh`).

Put TLS in front of it: a Cloudflare Tunnel (below), or Caddy/nginx on the same
machine with `TRACKSTAR_ADDR=127.0.0.1:3000`. After creating your accounts set
`TRACKSTAR_ALLOW_REGISTRATION=false` and `systemctl restart trackstar`.

## Proxmox LXC installation

Create the container yourself in the Proxmox UI or with `pct`; Trackstar does not
need anything unusual. Recommended (floor in brackets):

| | | Why |
|---|---|---|
| Template | Debian 13 standard (Ubuntu also works) | `setup.sh` supports Debian/Ubuntu |
| Type | **unprivileged** | Trackstar runs as the `trackstar` user without capabilities |
| Cores | 1 | idle CPU is ~0; a request is ~1 ms |
| RAM | **512 MB** (256) | trackstar uses 15–50 MB; the rest is Debian + headroom for `apt upgrade` |
| Swap | 512 MB (256) | safety net for upgrade spikes; unused in normal operation |
| Disk | 8 GB (4) | template ≈ 0.5 GB, the database is a few MB, backups are the size of the database |
| Features | none | **nesting is not required** — see below |
| Start at boot | yes | |

Then two steps, kept deliberately separate:

**1. Prepare the container (system level).** Inside the container as root,
after your own network setup — copy just this one file in, it is standalone:

```sh
./setup-lxc.sh --timezone Europe/Berlin --authorized-key ~/.ssh/id_ed25519.pub
```

`setup-lxc.sh` checks that it really is an LXC on Debian/Ubuntu (`--force` to
override), prints what the container provides (privileged?, cores, RAM/swap
limits from cgroups, free disk, whether mount namespaces work) and warns about
anything that would bite later (under 200 MB RAM, no IP, broken DNS), runs
`apt update`/`upgrade` (`--skip-upgrade` if you already did), installs the
base packages a slim template lacks (`ca-certificates curl tar
openssh-server …`), enables SSH, makes the journal persistent and, optionally,
sets the system time zone and authorizes an SSH key for root. It installs
**nothing application-specific** — no user, directories, config or service.

**2. Deploy the application.** From your workstation:

```sh
./scripts/deploy.sh root@<container-ip> -- --public-url https://track.example.com/ --timezone Europe/Berlin
```

On the first contact this runs `setup.sh` inside the container (binary,
`trackstar` user, `/etc/trackstar`, `/var/lib/trackstar`, systemd unit, session
secret); afterwards it only swaps the binary with snapshot and rollback. Everything
the application needs is the deploy's job, so a container prepared once never
needs revisiting when the application changes. (Without SSH you can also copy a
release archive in and run `sudo ./scripts/setup.sh …` yourself.)

**About nesting.** systemd's mount-namespace sandboxing (`ProtectSystem=`,
`PrivateTmp=`, …) is unavailable in an unprivileged container without the
*nesting* feature, so `setup.sh` probes for it with `systemd-run` and, only if
the probe fails, installs
`/etc/systemd/system/trackstar.service.d/10-no-namespaces.conf`, which turns off
just those directives. The service still runs as the unprivileged `trackstar`
user with no capabilities, inside an unprivileged container. If you prefer the
full sandbox, enable nesting on the container and re-run `setup.sh`; it removes
the drop-in when the probe succeeds.

## Docker installation

Optional. One container, no database container:

```sh
docker compose up -d --build      # or: make docker && docker compose up -d
```

Data is in `./data`. The image is `distroless/static` plus the binary (no
shell); the health check is `trackstar healthcheck`. It runs as root by default
so the bind mount works regardless of ownership; to drop root,
`chown 65532:65532 data` and uncomment `user:` in `docker-compose.yml`.
Backup: `docker compose exec trackstar trackstar backup /var/lib/trackstar/backup-$(date +%F).db`.

## Cloudflare Tunnel

```
Public hostname:  track.example.com
Origin service:   http://192.168.1.240:3000
Trackstar:          TRACKSTAR_PUBLIC_URL=https://track.example.com/
```

Trackstar does not read `X-Forwarded-Proto`/`-Host` at all: cookie security and
the CSRF origin check derive from `TRACKSTAR_PUBLIC_URL`. `CF-Connecting-IP` and
`X-Forwarded-For` are used only for the logged client IP and the login rate
limiter, and only when the TCP peer is listed in `TRACKSTAR_TRUSTED_PROXIES` —
direct clients cannot spoof them. If cloudflared runs on another machine, add
its address. Full walkthrough: [`deploy/cloudflared-example.md`](deploy/cloudflared-example.md).

## Backups and restore

```sh
sudo /opt/trackstar/scripts/backup.sh                  # → /var/backups/trackstar/trackstar-backup-YYYY-MM-DD-HHMMSS.tar.gz
sudo /opt/trackstar/scripts/backup.sh --output-dir /mnt/nas/trackstar --keep 14
sudo /opt/trackstar/scripts/restore.sh /var/backups/trackstar/trackstar-backup-….tar.gz
```

The archive holds a consistent `trackstar.db` (taken online with `VACUUM INTO`
and verified with `PRAGMA integrity_check`), `trackstar.env` with the session
secret blanked, a `MANIFEST`, and `uploads/` should that directory ever exist.
`--include-secrets` keeps the secret (restore with `--with-config` to bring the
configuration back too); without it a restore simply signs everyone out.

`restore.sh` validates the archive *before* touching anything, stops the
service, moves the current data to `/var/lib/trackstar/pre-restore-<timestamp>/`,
restores, fixes ownership, starts the service and checks `/health` — and puts
the previous data back if the restored service is unhealthy.

Nightly cron: `15 3 * * * /opt/trackstar/scripts/backup.sh --keep 14 >/dev/null`

## Updates and deploys

On the host:

```sh
sudo /opt/trackstar/scripts/update.sh                              # latest GitHub release (needs TRACKSTAR_REPO, see setup.sh --repo)
sudo /opt/trackstar/scripts/update.sh --version v0.2.0
sudo /opt/trackstar/scripts/update.sh --file trackstar-v0.2.0-linux-amd64.tar.gz
```

It verifies the download (SHA256SUMS when published, and that the binary runs
on this machine), snapshots the database, keeps the old binary as
`trackstar.previous`, swaps atomically, restarts, polls `/health` for 30 s, and
on failure restores both the binary and the pre-update snapshot (an older
binary must not meet a newer schema).

From your workstation: `./scripts/deploy.sh root@192.168.1.240` builds the
frontend and a Linux binary for the remote architecture, uploads it, snapshots
the database, stops the service, swaps the binary atomically, starts (migrating
on startup), verifies `/health`, and rolls back on failure. It never touches
`/etc/trackstar/trackstar.env`. Non-root SSH users need passwordless sudo.

## API

JSON over cookies; errors are `{"error": "…"}` with 401/403/404/409/422/429.

```
POST   /api/auth/register | /api/auth/login | /api/auth/logout
GET    /api/me            PATCH /api/me {display_name, current_password, new_password}
GET    /api/users         PATCH /api/users/:id {display_name, is_admin, is_active}   POST /api/users/:id/password   (admin)
GET    /api/config
GET    /api/projects      POST /api/projects
GET    /api/projects/:id  PATCH … DELETE …       (:id may be the numeric id or the slug)
GET    /api/projects/:id/stories[?q=text | ?section=done | ?section=deleted]
POST   /api/projects/:id/stories     {title, type?, estimate?, section?, description?, owner_id?, labels?}
GET    /api/projects/:id/labels | /iterations | /velocity
GET    /api/projects/:id/events      text/event-stream; events "stories" and "project", data {project_id, story_id, client}
GET    /api/stories/:id              (with comments and activity)
PATCH  /api/stories/:id              {title, description, type, state, estimate|null, owner_id|null, requester_id, labels}
DELETE /api/stories/:id              → the trashed story (soft delete, 30-day retention)
POST   /api/stories/:id/restore
POST   /api/stories/:id/move         {section, prev_id | next_id}   → {story, renormalized}
POST   /api/stories/:id/comments     {body}        DELETE /api/comments/:id
GET    /api/system/info              (admin)
GET    /health
```

`GET /api/projects/:id/velocity` →
`{"velocity":11,"average":11.0,"window":3,"estimated":false,"iterations":[{"number":12,"points":10},…]}`

## Resource usage

Measured on the first working build (linux/amd64, Go 1.27, 13 MB binary),
`VmRSS`/`VmHWM` from `/proc/<pid>/status`:

| | |
|---|---|
| Idle after a restart on an existing database | **15 MB** RSS |
| Idle after first start (migrations ran) and interactive use of the board | **27 MB** RSS |
| After a burst of 1 500 sequential API/asset requests (also the peak, `VmHWM`) | 45 MB — the Go runtime hands freed heap back lazily |
| Startup to listening (existing database) | ~3 ms |
| Idle CPU | 0 s of CPU accumulated while idle: no timers or background goroutines, only the HTTP listener |

Live streams (same build, measured with 50 idle `EventSource` connections):

| | |
|---|---|
| 50 idle streams | +4.6 MB RSS over baseline (≈ **90 KB per stream**: goroutine, HTTP buffers, subscriber channel) |
| 60 s idle with 50 streams (two heartbeats each) | 20 ms of CPU in total |
| 100 story moves broadcast to 50 streams | 0.36 s CPU (that is the moves themselves; a broadcast is microseconds), RSS peaked at 41 MB |
| all streams closed | 0 streams, 7 goroutines; RSS back to 34 MB within 20 s |

A board tab costs about as much as one extra HTTP keep-alive connection. Since
the stream replaces the old 60 s poll, idle load is lower than before.

Targets were < 64 MB idle, < 128 MB typical. Most of the footprint is the Go
runtime plus the pure-Go SQLite engine; the page cache is capped at 8 MB.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Exits at once with `invalid configuration:` | Every bad `TRACKSTAR_*` value is listed; fix `/etc/trackstar/trackstar.env`. `journalctl -u trackstar -n 50` |
| `status=226/NAMESPACE` in an LXC | Sandbox needs mount namespaces. Re-run `setup.sh` (installs the drop-in) or enable nesting. |
| Login "works" but you are signed out immediately | `TRACKSTAR_PUBLIC_URL` is `https://…` but you browse over plain `http://` — Secure cookies are not stored. Use the public URL, or set an `http://` URL for LAN-only installs. |
| `403 cross-origin request rejected` | The page's origin is neither `TRACKSTAR_PUBLIC_URL` nor the request's Host. Fix the public URL; make your reverse proxy pass `Host` through. |
| Every request logs the proxy's IP | Add the proxy to `TRACKSTAR_TRUSTED_PROXIES`. |
| `429` on login | 20 attempts per 5 minutes per client IP; wait, or restart the service. |
| "this account has been deactivated" | An administrator deactivated the account; another admin can reactivate it under *Users*. |
| Forgotten password | On the server: `sudo -u trackstar sh -c 'set -a; . /etc/trackstar/trackstar.env; exec trackstar reset-password you@example.com'` (reads the new password from stdin, revokes sessions). |
| "registration is disabled" | `TRACKSTAR_ALLOW_REGISTRATION=false` and an account exists. Enable it briefly to add a teammate. |
| `database is locked` | Another process holds a long write lock (an open `sqlite3` shell?). Trackstar waits 5 s. |
| `attempt to write a readonly database` after running tools as root | Root created `-wal`/`-shm` files: `chown -R trackstar:trackstar /var/lib/trackstar`. The scripts avoid this by running as `trackstar`. |
| Top-bar dot stays red / changes don't appear live | The stream is being cut: a proxy buffering or timing out `text/event-stream` (nginx: `proxy_buffering off; proxy_read_timeout 1h;` for `/api/*/events`). The board still refreshes on focus and every 5 min. |
| `make dev`: "./data-dev is not writable" | Something else (e.g. a container) owns that directory. `sudo chown -R $USER: data-dev`, or `make dev DEV_DATA_DIR=<other dir>`. `make dev` refuses to start in this state and stops both processes if either exits. |
| `make dev`: "Port 5173 is in use" | Another Vite (possibly from an earlier `sudo make dev`) still owns the port: `fuser -k 5173/tcp` (or `sudo`), or `make dev DEV_PORT=5180`. |
| Blank page: "built without the frontend" | The binary was built before `make frontend`; use `make build`. |
| Iteration boundaries feel off by hours | Set `TRACKSTAR_TIMEZONE` (e.g. `America/Los_Angeles`) and restart. |

## Known limitations

- Single team: every user sees every project; the only role is administrator.
  No e-mail, so a forgotten password is reset by an administrator (*Users*) or
  on the server (see Troubleshooting).
- Changing a project's iteration length or start weekday renumbers past
  iterations (they are derived, not stored).
- Live updates are per process: running two instances behind one load
  balancer would need a shared bus (PostgreSQL `LISTEN/NOTIFY`).
- Search is a substring match (`%` and `_` act as wildcards); no ranking.
- Deleted stories are purged 30 days after deletion; there is no archive
  beyond that.
- No attachments, epics, tasks, story blockers or notifications.
- PostgreSQL is designed for but not implemented.
- The Svelte UI is desktop-first; on narrow screens the panels scroll sideways.
