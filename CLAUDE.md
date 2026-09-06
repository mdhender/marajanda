# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Required reading

[AGENTS.md](AGENTS.md) is authoritative for workflow: work prioritization, the
beta version-control rules (commit to `main`, bump `version.go` with every code
change), coding directives, and verification expectations. Read it before
changing anything.

The reference documents own the contracts the code implements. Consult the one
that covers the area you touch, and do not silently reinterpret it:

- [docs/PRODUCT.md](docs/PRODUCT.md) — roles, factions, faction-name rules, map coordinates.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — stack, package boundaries, flag/env table, server lifecycle.
- [docs/DATASTORE.md](docs/DATASTORE.md) — SQLite identity, open modes, migrations, seed data.
- [docs/ACCOUNTS.md](docs/ACCOUNTS.md) — identity, sessions, invitations, registration.
- [docs/reference/](docs/reference/) — `player-origin.md`, `terrain.md`, `glossary.md`.

Ask the user before deciding game rules or server behavior these documents do
not specify.

## Commands

```sh
go test ./...                                   # all packages
go test ./internal/datastore -run TestAuthenticate -v
go test -tags production ./...                  # covers the production-only build (agents_production*.go)
go test ./internal/prng -update                 # regenerate testdata/golden.json — see below
go vet ./...
go run ./cmd/marajanda --root :memory: --game-seed 1,2 --port 18999 --timeout 1s
```

Prefer `go run ./cmd/marajanda` over `go build`; a stray `marajanda` binary in
the repo root is build litter. The only intentional build is the production
one: `go build -tags production ./cmd/marajanda`.

`--root :memory:` needs `--game-seed` but no admin seed values; it seeds
`admin@marajanda.com` / `player@marajanda.com`, both with password `good.luck`.

### Running the dev server

`overmind start` runs `air -c .air.toml`, which rebuilds and restarts the daemon
on Go changes. Configuration comes from `.env.development.local` via
`internal/dotenv`, not from Overmind.

The daemon speaks plain HTTP on `127.0.0.1:18443`. A machine-wide Caddy
(`brew services start caddy`) terminates TLS for `https://htmx-app.localhost:8443`
and proxies every path to it. **Always browse through Caddy** — the session
cookie is `Secure`, so any signed-in flow silently fails on the raw port.

To get an authenticated browser session in a non-production build with `ENV`
not `production`:

```
https://htmx-app.localhost:8443/__agents/log-me-in/agent%40example.test?returnTo=%2Fplayer%2Fdashboard
```

That route creates the player account if needed and starts a real browser
session; `curl` does not authenticate a browser.

`MARAJANDA_ROOT` (currently `data/beta`) is git-ignored and must exist before
the daemon starts; it creates `marajanda.db` inside.

## Architecture

Dependency direction is one-way: `cmd/marajanda` → `internal/server` →
`internal/datastore` → `internal/game` → `internal/prng`.

- **`cmd/marajanda`** — `main` loads dotenv for `ENV` (default `development`)
  *before* `ff/v4` parses flags, so tests can supply isolated environments.
  Flags override `MARAJANDA_*` environment variables. `Exec` only assembles
  `server.Config` and calls `server.Run`; keep it thin.
- **`internal/server`** — `Run` opens the store (chdir'ing into the root for a
  persistent database), listens, and shuts down gracefully on context cancel or
  `--timeout`. `handler.go` holds the whole web layer: routes on
  `http.ServeMux`, in-memory sessions keyed by a `Secure`/`HttpOnly`/`SameSite=Lax`
  cookie, and **the entire UI as one `html/template` in a Go string literal**
  (`pageTemplate`) switched by `pageData.View`. There is no template directory,
  no static assets, and no embed — that is why `.air.toml` watches only `.go`.
  The mux is wrapped in `http.CrossOriginProtection`, and `render` sets the CSP
  and related headers.
- **Build tags** — `agents_development.go` (`!production`) registers
  `GET /__agents/log-me-in/{email}`; `agents_production.go` (`production`) is a
  no-op. The route is also suppressed when `ENV=production` in a non-production
  build. Both paths have tests, so the production tag needs its own test run.
- **`internal/datastore`** — one `sqlitemigration.Pool` plus a held `*sqlite.Conn`.
  `Open` (persistent directory), `OpenMemory`, and `OpenSharedMemory` are three
  distinct modes; tests exercise both in-memory modes wherever connection
  sharing matters. During beta `schema.Migrations` is a **single squashed
  baseline** — amend it and delete existing databases rather than appending a
  migration. Creating the database generates the world and inserts every hex;
  `hexes` is the whole map, not a list of account origins, so the origin
  exclusion set comes from `accounts` (deferred FK from `accounts` to `hexes`
  now asserts an origin is a real hex of the world).
- **`internal/game`** — pure deterministic rules over `github.com/maloquacious/hexg`:
  `GenerateWorld`, `AssignOrigin`, `WindowView`/`PlayerView`, faction-name
  normalization. No database or HTTP. `GenerateWorld` builds the whole bounded
  world in one pass — sea level is a percentile of the entire field, a lake is
  water the flood fill from the rim never reaches, and a rain shadow needs an
  upwind neighbour — so terrain is generated once and stored, never recomputed
  per hex.
- **`internal/prng`** — the determinism foundation. Read `internal/prng/doc.go`
  before touching anything downstream of it.

### The determinism contract

Randomness is addressed, not consumed: `seeds.Roller(tag, keys...)` hashes the
two game seeds with a key path to derive a private stream, so the same seeds
produce the same world regardless of draw order or map iteration order.

- Instance keys must be **intrinsic** — hex coordinates, an email digest, a
  player number — **never SQLite row ids**, which depend on insertion order.
- Domain tags in `tags.go` are append-only and start at 1. Never insert or
  reorder them; `iota` would renumber every later tag and rewrite every live game.
- The key-path encoding and tag numbering are a compatibility surface,
  "slushy" in beta and frozen after alpha. `testdata/golden.json` pins
  (seeds, path) → output. If a golden test fails, the change broke
  determinism; only run `-update` for a deliberate, reviewed change.

Game seeds are two comma-separated int64s (`--game-seed 98374,-98`), required
whenever a database is created and immutable afterwards. `--width` (default
255, range 20..511) and `--height` (default 127, range 20..255) join them: they
are half-extents, so the default world is 511 by 255, and the world is generated
from all four and is fixed once written.

## Conventions

- Go 1.26. Code uses current idioms (`cmp.Or`, `range` over int, `new(T{...})`,
  `t.Context()`); match them.
- Every Go file starts with `// Copyright (c) 2026 Michael D Henderson.`
- `internal/cerrs.Error` provides constant sentinel errors (used in `dotenv`); other packages use `errors.New` package vars. Follow the surrounding package.
- Server-rendered HTMX first; add Alpine.js only when HTMX cannot do the job.
- Screenshots of UI work land in `out/` (git-ignored).
