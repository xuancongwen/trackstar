# Trackstar

A lightweight, self-hosted project tracker in the spirit of Pivotal Tracker:
icebox, backlog, iterations, points and velocity on a keyboard-friendly board.
One Go binary, one SQLite file, no other services, with a REST API and an MCP
server so AI agents can work in your projects too.

Source: <https://github.com/xuancongwen/trackstar> · MIT licensed.

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
- [Database portability](#database-portability)
- [DigitalOcean / any Debian-Ubuntu server](#digitalocean--any-debianubuntu-server)
- [Proxmox LXC installation](#proxmox-lxc-installation)
- [Docker installation](#docker-installation)
- [Cloudflare Tunnel](#cloudflare-tunnel)
- [Backups and restore](#backups-and-restore)
- [Updates and deploys](#updates-and-deploys)
- [API](#api)
- [MCP for AI agents](#mcp-for-ai-agents)
- [Resource usage](#resource-usage)
- [Troubleshooting](#troubleshooting)
- [Known limitations](#known-limitations)
- [Contributing](#contributing)
- [License](#license)

## Using it

The first account you register becomes the administrator. The projects page
lists your projects with what is going on in each (stories in progress,
stories delivered and waiting to be accepted, when it was last active). Each
project carries a small chart of its last 30 days, and the busiest projects
come first; the sort control switches to alphabetical order and your browser
remembers the choice. Type in the search box (`/` jumps to it) to narrow the
list; when one project is left, Enter opens it. Create a project and you
land on the board:

- **Icebox** – ideas. **Backlog** – prioritised work. **Current iteration** –
  what is being worked on now. **Done** (toggle) – accepted work by iteration.
  The project setting *Combine icebox and backlog* shows the icebox at the
  bottom of the backlog panel, under an *Icebox* divider, for a two-column
  board; stories keep their section. To have every project you create start
  that way, tick *Combine icebox and backlog* under *your name ▸ Account ▸
  New projects*.
- Drag rows to prioritise, or drag them between panels. Work that is in
  progress (started/finished/delivered/rejected) stays in the current iteration.
- Each row has the one button that matters next: Start → Finish → Deliver →
  Accept/Reject → Restart. Unestimated features show the point scale
  (0 1 2 3 5 8) instead of *Start*; bugs and chores carry no points unless the
  project setting *Allow points on bugs and chores* is on, and may always
  stay unestimated.
- The backlog is cut into projected iterations using the velocity: the average
  of accepted points over the last N completed iterations
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
- **Epics** (`e` or the Epics button) are labels with a description and a
  progress bar (accepted / total feature points); click one to filter the board
  to it. Stories join an epic by carrying its label.
- **Tasks** are a checklist inside a story (`☑ 2/5` on the row). **Blockers**
  ("blocked by #12") show ⛔ on the row and a count in the Current header while
  any blocker is unaccepted; they never prevent state changes.
- **Filters** are evaluated instantly in the browser: free text plus
  `owner:me`, `requester:kim`, `type:bug`, `state:started`, `estimate:3`,
  `label:auth` / `epic:auth`, `is:blocked`, `is:unestimated`, `has:tasks`,
  `-type:chore`, `"exact phrase"`. The ▾ next to the filter box has built-ins
  (My work, Unestimated, Blocked, Bugs) and lets you save your own per project.
- **Multi-select**: `x` or Shift-click marks rows; dragging a marked row moves
  the whole set (in board order, one transaction); `Shift+I` / `B` / `C` sends
  the selection to the end of Icebox / Backlog / Current.
- **Done** shows an accepted-points-per-iteration chart with the velocity line
  and pages through older iterations.
- **Members** (Settings): whoever creates a project owns it and adds the
  others. Owners manage members and settings and may archive or delete the
  project; members read and write stories; administrators always have access.
  Projects you are not in are not listed. A project with no members at all
  (from before ownership existed) stays open to everyone until an
  administrator adds an owner.
- **Archive or delete** (Settings, owners and administrators): an archived
  project stays readable, keeps its history and velocity, and is listed under
  *Archived*, but nobody can change stories until it is unarchived. Deleting
  removes the project with all of its stories, epics, comments and history,
  immediately and for good; type the project's name to confirm.
- **On a phone** (≤ 900 px) the board shows one panel at a time with a tab
  strip (Icebox · Backlog · Current · Epics · Done · Trash), a full-width
  filter box, a ☰ menu for settings/account, and a floating **+** that creates
  in the visible panel. Reordering is hold-to-drag (a quick swipe scrolls);
  moving between panels is done from the story's **Move to** buttons. The
  drawer fills the screen.
- Your name and password live under *your name ▸ Account*, as do your **API
  tokens**: a token acts as you for scripts and MCP clients (send it as
  `Authorization: Bearer tst_…`). The secret is shown once; revoke it from the
  same dialog. Tokens cannot change passwords, manage accounts or mint tokens.
  **Connected apps** lists the MCP clients you approved by signing in (see
  [MCP for AI agents](#mcp-for-ai-agents)); *Disconnect* cuts one off.
  Administrators get
  *Users*: rename, promote/demote, deactivate/reactivate, reset a password.
  Administrators can also **Reopen** an accepted story (it drops out of
  velocity history).

| Key | Action |
|---|---|
| `c` | new story (in the selected story's panel; `Shift+Enter` saves and keeps the dialog open) |
| `x` / Shift-click | add the row to the selection; `Shift+I` / `B` / `C` move the selection |
| `e` | epics sidebar |
| `j` / `k` (or ↓ / ↑) | move selection |
| `h` / `l` (or ← / →) | switch panel |
| `Enter` | open selected story |
| `/` | filter box |
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
internal/api/         handlers, middleware (request id, logging, origin check, trusted proxies), SSE stream, OAuth endpoints
internal/mcpserver/   MCP tools and resources over the same services, mounted at /mcp behind the same auth
internal/events/      in-process change hub: Publish(project) → every open stream of that project
internal/auth/        bcrypt passwords, server-side sessions (HMAC-hashed tokens), login rate limit, API tokens, OAuth (oauth.go)
internal/config/      TRACKSTAR_* environment → validated Config
internal/database/    Open/Migrate/InTx/Backup; sqlite.go is the only driver-specific file
internal/story/       stories, workflow (states.go), ordering (position.go), comments, labels, search
internal/project/     projects and their iteration settings
internal/iteration/   Schedule → iteration N, iteration containing t
internal/velocity/    velocity + iteration history from accepted stories
db/                   migrations, queries, sqlc.yaml
web/                  Svelte app; web/embed.go embeds web/dist
deploy/               setup.sh (prepare a machine and install), deploy.sh (build and ship from a checkout); unit, env examples
scripts/              run on the server: update, backup, restore
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
- **API tokens** (`tst_…`) are stored the same way and checked before the
  cookie, so a script never acts as whoever is signed in to the browser. A
  bearer request skips the login rate limiter (it carries no password; a miss
  is one indexed lookup) and is refused on the account and token routes, so a
  leaked token is contained to the project data its owner can reach.
  Deactivating a user disables their tokens at once; a password change does
  not (revoke them from *Account*).
- **Live updates without websockets or workers.** Every successful write
  publishes a tiny "project N changed" event to an in-process hub; each open
  board holds one Server-Sent Events stream (`GET /api/projects/:id/events`)
  and refetches the story list on receipt (debounced, echoes of its own
  changes ignored, deferred while the user is mid-drag). `EventSource`
  reconnects on its own and every reconnect refetches, so nothing is missed.
  A 30 s heartbeat keeps proxies (Cloudflare: 100 s idle limit) from closing
  the stream. Focus-refresh and a 5-minute poll remain as a safety net.
  The hub is single-process by design.
- **Projects belong to their creator.** Creating a project makes the caller
  its owner in the same transaction, so a project is members-only from birth.
  Owners manage members, settings and deletion; members read and write; a
  project must always keep one owner. Administrators have full access
  everywhere. Hidden projects answer 404, so membership does not leak which
  projects exist. Every handler checks access through one helper
  (`internal/api/access.go`) at one of three levels: read, write, manage.
  Projects that predate ownership and have no members stay open to everyone;
  only an administrator can add their first owner.
- **Epics are labels** with `is_epic` set; progress is computed from live
  stories at read time. Demoting an epic keeps the label on its stories.
- **Filtering is client-side.** The board already holds every active story,
  so the query language (`web/src/lib/filter.ts`) runs locally and instantly;
  the server's `?q=` remains for API clients.

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
search, trash/undo, the account dialog, — with two browsers — live sync, and
(with iPhone emulation and touch input) the phone layout: tabs, FAB, drawer
"Move to", hold-to-drag reordering, and that nothing scrolls sideways, zooms
on focus or ends up out of reach at 390px, 320px and in landscape. It also
covers the OAuth flow and the projects page.

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
/opt/trackstar/scripts/           update.sh, backup.sh, restore.sh
/opt/trackstar/deploy/            setup.sh (re-run to change options), unit and env examples
```

The binary serves the frontend on `/`, the API on `/api/*`, MCP on `/mcp`,
OAuth for MCP clients on `/oauth/*` and `/.well-known/oauth-*`, and
`GET /health` (`{"status":"ok"}`, checks the database, no authentication).

## Configuration

Environment only; invalid values abort startup with a list of every problem.
See [`deploy/trackstar.env.example`](deploy/trackstar.env.example).

| Variable | Default | |
|---|---|---|
| `TRACKSTAR_ADDR` | `127.0.0.1:3000` | listen address |
| `TRACKSTAR_DATA_DIR` | `./data` | created if missing |
| `TRACKSTAR_DATABASE_DRIVER` | `sqlite` | `postgres` is reserved |
| `TRACKSTAR_DATABASE_URL` | `$DATA_DIR/trackstar.db` | |
| `TRACKSTAR_PUBLIC_URL` | `http://localhost:<port>/` | decides Secure cookies and the allowed `Origin`; also the OAuth issuer, so it must be exactly the URL clients reach |
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

## Database portability

SQLite is the only supported engine. The application layer does not depend
on it, though: all it uses is `database.Store` (`dbgen.Querier` + `InTx`),
and the schema and queries follow these rules so that they stay portable:

- explicit 64-bit integer primary keys, `RETURNING` instead of last-insert-id;
- timestamps are UTC unix seconds in `BIGINT` columns (no driver-specific time
  handling); booleans are `BOOLEAN`;
- no triggers, generated columns, or SQL-side business logic — iterations,
  velocity and ordering are computed in Go;
- queries use `sqlc.arg()` / `sqlc.narg()` / `sqlc.slice()`, never `?` or `$1`;
- search is `LOWER(title || ' ' || description) LIKE …`, hidden behind
  `story.Service.List`.

## DigitalOcean / any Debian-Ubuntu server

A 512 MB droplet is plenty. On your workstation:

```sh
make release
scp dist/trackstar-*-linux-amd64.tar.gz root@droplet:
```

On the server:

```sh
tar xzf trackstar-*-linux-amd64.tar.gz && cd trackstar-*-linux-amd64
sudo ./deploy/setup.sh --public-url https://track.example.com/ --port 3000
```

Or do both in one step from your checkout (first run installs, later runs
deploy): `./deploy/deploy.sh root@droplet -- --public-url https://track.example.com/`

`setup.sh` is the one script that turns a fresh machine into a running
Trackstar, the same on an LXC, a VM or a droplet. It:

- validates the distribution and prints what the machine provides (container
  or not, privileged?, cores, RAM, swap, free disk), warning about anything
  that would bite later (under 200 MB RAM, no IP address);
- installs the base packages a slim template lacks (`ca-certificates curl tar
  tzdata openssh-server …`), enables SSH and makes the journal persistent. A
  first install also runs `apt-get upgrade` (`--skip-upgrade` to leave that
  to you);
- optionally sets the time zone (`--timezone`, used for the system clock and
  for iteration boundaries) and authorizes an SSH key for root
  (`--authorized-key`);
- creates the `trackstar` user, `/etc/trackstar` and `/var/lib/trackstar`,
  installs the binary, writes `trackstar.env` (generating a session secret),
  installs and enables the hardened systemd unit, starts it and waits for
  `/health`.

It is idempotent: re-running keeps configuration, secret and data, only
changes the options you pass explicitly, and does not upgrade the system
again. Other sources for the binary: `--binary PATH`, `--release-url URL`,
`--repo OWNER/NAME [--version TAG]` (GitHub releases, e.g.
`--repo xuancongwen/trackstar`; the repo is remembered for `update.sh`).

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

Give the container your SSH public key when you create it (the *SSH public
key* field in the Proxmox dialog, or `pct create … --ssh-public-keys`). Then
one command from your workstation:

```sh
./deploy/deploy.sh root@<container-ip> -- --public-url https://track.example.com/ --timezone Europe/Berlin
```

On the first contact this runs `setup.sh` inside the container, which
prepares the system and installs the application (see the previous section
for the list). Afterwards the same command only swaps the binary, with
snapshot and rollback, so the container never needs revisiting when the
application changes.

No SSH access yet? Copy `deploy/setup.sh` into the container (it is
standalone for this) and run it in the console as root:

```sh
./setup.sh --authorized-key "ssh-ed25519 AAAA… you@workstation"
```

With no binary to install it prepares the system, authorizes the key and
prints the deploy command to run next. Without a workstation at all, copy a
release archive in and run `sudo ./deploy/setup.sh …` there, as on any server.

**About nesting.** systemd's mount-namespace sandboxing (`ProtectSystem=`,
`PrivateTmp=`, …) is unavailable in an unprivileged container without the
*nesting* feature, so `setup.sh` probes for it with `systemd-run` and, only if
the probe fails, installs
`/etc/systemd/system/trackstar.service.d/10-no-namespaces.conf`, which turns off
just those directives. The service still runs as the unprivileged `trackstar`
user with no capabilities, inside an unprivileged container. If you prefer the
full sandbox, enable nesting on the container and re-run
`sudo /opt/trackstar/deploy/setup.sh`; it removes
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

If MCP clients connect with OAuth (a Claude connector does), their servers
call `/mcp`, `/.well-known/*`, `/oauth/register`, `/oauth/token` and
`/oauth/revoke` directly, with no browser. Keep those paths out of Cloudflare
Access policies, managed challenges and Bot Fight Mode, or the connection
fails before Trackstar sees it. `/oauth/authorize` is opened in the user's
browser and can stay behind whatever protects the rest of the site.

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
configuration back too); without it a restore simply signs everyone out and
invalidates every API token and OAuth connection (they are keyed with the
same secret), so scripts need new tokens and connected apps must be
authorized again.

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

From your workstation: `./deploy/deploy.sh root@192.168.1.240` builds the
frontend and a Linux binary for the remote architecture, uploads it, snapshots
the database, stops the service, swaps the binary atomically, starts (migrating
on startup), verifies `/health`, and rolls back on failure. It never touches
`/etc/trackstar/trackstar.env`. Non-root SSH users need passwordless sudo.
To skip typing the host, copy `deploy/deploy.env.example` to `deploy/deploy.env`
(gitignored, never uploaded), set `DEPLOY_TARGET=user@host.lan`, and run
`./deploy/deploy.sh` with no target.

## API

JSON over cookies or `Authorization: Bearer <api token>`; errors are
`{"error": "…"}` with 401/403/404/409/422/429. Routes marked *(session)*
refuse bearer tokens.

```
POST   /api/auth/register | /api/auth/login | /api/auth/logout
GET    /api/me            PATCH /api/me {display_name, current_password, new_password}   (session)
GET    /api/me/tokens     POST /api/me/tokens {name, expires_in_days?} → {…, token}   DELETE /api/me/tokens/:id   (session)
GET    /api/me/grants     DELETE /api/me/grants/:id   (session; apps connected through OAuth)
GET    /api/users         PATCH /api/users/:id {display_name, is_admin, is_active}   POST /api/users/:id/password   (admin, session)
GET    /api/config
GET    /api/projects      POST /api/projects
GET    /api/overview      {projects: [{project_id, in_progress, to_accept, last_activity_at, activity: [changes per day, last 30 days]}]}   (the projects page)
GET    /api/projects/:id  PATCH … DELETE …       (:id may be the numeric id or the slug; DELETE removes every story with it)
                          PATCH {name, description, iteration_length_days, iteration_start_weekday, velocity_window, estimate_bugs_and_chores}
POST   /api/projects/:id/archive     DELETE …    (archive = read-only for everyone; owners and admins)
GET    /api/projects/:id/stories[?q=text | ?section=done | ?section=deleted]
POST   /api/projects/:id/stories     {title, type?, estimate?, section?, description?, owner_id?, labels?}
GET    /api/projects/:id/labels | /iterations | /velocity
GET    /api/projects/:id/epics       POST … {name, description}     PATCH /api/epics/:id     DELETE /api/epics/:id (demotes to a label)
GET    /api/projects/:id/members     PUT /api/projects/:id/members/:user {role: owner|member}      DELETE …   (owners and admins)
GET    /api/projects/:id/filters     POST … {name, query}           DELETE /api/filters/:id   (per user)
POST   /api/stories/move             {ids, section, prev_id | next_id}   → {stories}   (ordered bulk move, one transaction)
POST   /api/stories/:id/tasks {description}     PATCH /api/tasks/:id {description, done, position}     DELETE /api/tasks/:id
GET    /api/projects/:id/events      text/event-stream; events "stories" and "project", data {project_id, story_id, client}
GET    /api/stories/:id              (with comments, activity and tasks)
PATCH  /api/stories/:id              {title, description, type, state, estimate|null, owner_id|null, requester_id, labels, blocked_by}
DELETE /api/stories/:id              → the trashed story (soft delete, 30-day retention)
POST   /api/stories/:id/restore
POST   /api/stories/:id/move         {section, prev_id | next_id}   → {story, renormalized}
POST   /api/stories/:id/comments     {body}        DELETE /api/comments/:id
GET    /api/system/info              (admin)
GET    /health
```

`GET /api/projects/:id/velocity` →
`{"velocity":11,"average":11.0,"window":3,"estimated":false,"iterations":[{"number":12,"points":10},…]}`

The OAuth endpoints exist for MCP clients (next section) and follow their
RFCs rather than the conventions above: errors are
`{"error": "invalid_grant", "error_description": "…"}`.

```
GET    /.well-known/oauth-protected-resource     RFC 9728: /mcp is guarded by this server
GET    /.well-known/oauth-authorization-server   RFC 8414: the endpoints below
POST   /oauth/register     RFC 7591 {client_name, redirect_uris} → {client_id, …}   (open, rate limited)
GET    /oauth/authorize    the consent screen, in the browser; PKCE S256 required
POST   /oauth/token        form-encoded; grant_type=authorization_code | refresh_token
POST   /oauth/revoke       RFC 7009 {token}
GET    /api/oauth/authorize   POST /api/oauth/authorize   (session; what the consent screen calls)
```

## MCP for AI agents

The same binary serves a [Model Context Protocol](https://modelcontextprotocol.io)
endpoint at `/mcp` (Streamable HTTP, stateless), so Claude Code, Claude
Desktop or any MCP client can read and update the board. It is a thin layer
over the same services as the REST API: identical access rules, validation,
activity log and live updates (open boards refresh when an agent changes a
story).

### Setting up access

There are two ways in. Both act as you: the agent sees the projects you see
and its changes are logged under your name.

**Sign in (OAuth).** A client that supports MCP authorization needs only the
URL. It discovers the rest from the 401 it gets, registers itself, and opens
Trackstar in your browser, where you sign in and approve it.

- Claude (web, desktop, mobile): *Settings ▸ Connectors ▸ Add custom
  connector*, enter `https://track.example.com/mcp`, then *Connect*. The
  connector belongs to your Claude account, so it is available everywhere you
  use Claude. Claude's servers make the calls, so the instance has to be
  reachable from the internet (see [Cloudflare Tunnel](#cloudflare-tunnel)).
- Claude Code:

  ```sh
  claude mcp add trackstar --transport http https://track.example.com/mcp
  ```

  then run `/mcp` in a session and authenticate. (`--scope user` makes it
  available in every project.)

The consent screen names the app and shows the host your browser is sent to
afterwards. The name is whatever the app calls itself, so approve only a
request you started. Approved apps are listed under *your name ▸ Account ▸
Connected apps*; **Disconnect** stops one on its next call. An app holds an
access token that lasts an hour and a refresh token that is replaced on every
use; a connection left unused for 60 days ends by itself. Changing or
resetting your password disconnects every app, as does deactivating the
account.

`TRACKSTAR_PUBLIC_URL` must be the URL the client was given: the discovery
documents and the token audience are built from it.

**Personal API token.** For scripts, curl and clients without OAuth support.
The token is the whole credential, so treat it like a password.

1. Sign in to Trackstar in a browser and open *your name ▸ Account*.
2. Under **API tokens**, give the token a name (e.g. `claude-code`), pick an
   expiry (or none) and press **Create**. Copy the `tst_…` secret now; it is
   shown once.
3. Register the endpoint with your client. For Claude Code:

   ```sh
   claude mcp add trackstar --transport http https://track.example.com/mcp \
     --header "Authorization: Bearer tst_…"
   ```

   For a client that only takes a JSON config or speaks stdio, bridge it
   with `mcp-remote`:

   ```json
   {
     "mcpServers": {
       "trackstar": {
         "command": "npx",
         "args": ["-y", "mcp-remote", "https://track.example.com/mcp",
                  "--header", "Authorization: Bearer tst_…"]
       }
     }
   }
   ```

4. Check it from a shell; an authenticated `initialize` answers with the
   server info, an unauthenticated one with 401:

   ```sh
   curl -s https://track.example.com/mcp \
     -H "Authorization: Bearer tst_…" -H "Content-Type: application/json" \
     -H "Accept: application/json, text/event-stream" \
     -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
   ```

To cut an agent off, revoke the token in the same dialog; its next call
fails. Give each agent or machine its own token so one can be revoked
without disturbing the others. Tokens work through the Cloudflare Tunnel
like any other request; nothing else needs to be opened.

Tools (a project is named by numeric id or slug):

| Tool | What it does |
|---|---|
| `list_projects` | projects you can see, with iteration settings; `include_archived` adds archived (read-only) ones |
| `list_users` | ids → names, so `owner_id` can be resolved; includes `me` |
| `list_stories` | a project's stories in board order; `section` (icebox, backlog, current, done) and `query` filters |
| `get_story` | one story with comments, tasks and activity |
| `create_story` | title, type, estimate, section, owner, labels |
| `update_story` | any field or the workflow `state`; `clear_owner` / `clear_estimate` to unset |
| `move_story` | to a section, after `prev_id` or before `next_id`, or to the top |
| `move_stories` | up to 50 stories of one project to one place, in the order given; each story succeeds or fails on its own and the result reports each |
| `add_comment` | |
| `list_epics` | epics with progress |
| `velocity` | velocity plus per-iteration history |

Resources: `trackstar://projects/{project}/current` and `…/backlog` return
the section as JSON for attaching as context. The server's instructions
explain sections, the workflow and move semantics to the model.

Deliberately absent: delete/restore, epic and member management, anything
about accounts or tokens. Errors come back as tool errors with the API's
message (`story 42 not found`, `project apollo not found` for one the acting
user is not a member of),
so the model can correct itself. Each tool call is one HTTP request with no
server-side session: nothing to keep alive through the tunnel, nothing lost
on a restart, and a revoked token or disconnected app stops the agent on its
next call.

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

The MCP server adds 2 MB to the binary (13.3 → 15.4 MB) and nothing at idle:
the tool table is built once at startup, and a stateless tool call is one
HTTP request that costs the same as the equivalent API call.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Exits at once with `invalid configuration:` | Every bad `TRACKSTAR_*` value is listed; fix `/etc/trackstar/trackstar.env`. `journalctl -u trackstar -n 50` |
| `status=226/NAMESPACE` in an LXC | Sandbox needs mount namespaces. Re-run `sudo /opt/trackstar/deploy/setup.sh` (installs the drop-in) or enable nesting. |
| Login "works" but you are signed out immediately | `TRACKSTAR_PUBLIC_URL` is `https://…` but you browse over plain `http://` — Secure cookies are not stored. Use the public URL, or set an `http://` URL for LAN-only installs. |
| `403 cross-origin request rejected` | The page's origin is neither `TRACKSTAR_PUBLIC_URL` nor the request's Host. Fix the public URL; make your reverse proxy pass `Host` through. |
| Every request logs the proxy's IP | Add the proxy to `TRACKSTAR_TRUSTED_PROXIES`. |
| `429` on login | 20 attempts per 5 minutes per client IP; wait, or restart the service. |
| Cloudflare error 1016 (Origin DNS error) on the tunnel hostname | The zone's DNS record for the hostname does not point at the running tunnel (`CNAME <tunnel-uuid>.cfargotunnel.com`). Delete the record and re-add the public hostname in the tunnel's settings, which recreates it. |
| A project disappeared from the list | You are not a member; ask one of its owners or an administrator to add you (Settings ▸ Members). |
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

- Permissions are deliberately simple: administrator, and per-project
  owner/member. No read-only role, no organisations. No e-mail, so a forgotten password is reset
  by an administrator (*Users*) or on the server (see Troubleshooting).
- Changing a project's iteration length or start weekday renumbers past
  iterations (they are derived, not stored).
- Live updates are per process: boards connected to one instance do not see
  changes made through another, so run a single instance.
- Search is a substring match (`%` and `_` act as wildcards); no ranking.
- Deleted stories are purged 30 days after deletion; there is no archive
  beyond that. Deleting a project is immediate and unrecoverable (archive it
  instead to keep it readable); restore from a backup if it was a mistake.
- No attachments, sub-epics, or notifications. Blockers are informational.
- SQLite is the only database engine; PostgreSQL is not supported.
- OAuth has no scopes: a connected app has its user's full project access,
  like an API token. Clients are public (PKCE, no client secret), register
  themselves, and may redirect only to https or a loopback address, so an
  app that needs a custom URI scheme cannot connect.
- On phones there is no cross-panel drag (use the drawer's Move to) and no
  multi-select; keyboard shortcuts need a keyboard.

## Contributing

Bug reports and pull requests are welcome at
<https://github.com/xuancongwen/trackstar/issues>. Before opening a pull
request run `make lint test`, and `make sqlc` if you changed `db/queries`
(CI fails when `internal/database/dbgen` is stale); `make build e2e` covers the
browser tests if you have Chrome or Chromium installed. Keep changes focused,
and add or update a test where one is practical. Never edit an applied
migration; add a new one (see [Development setup](#development-setup)).

Security issues: please do not open a public issue; see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE).
