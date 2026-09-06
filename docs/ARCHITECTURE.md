# Architecture Reference

## Application stack

- Go implements the application.
- HTMX provides server-rendered web interactions. Version `2.0.10`, vendored under `internal/server/assets/` and embedded in the binary.
- Alpine.js may provide client-side behavior that HTMX cannot reasonably supply; it is not required by default and is not present in the codebase.
- ZombieZen SQLite provides persistence and schema migrations.

## Page delivery

The web layer is one `html/template` in a Go string literal in
`internal/server/handler.go`. There is no template directory. The page a handler
renders is chosen by `pageData.View`; a named block of the same template can be
rendered on its own as a fragment.

### Assets

Third-party scripts are vendored under `internal/server/assets/` and embedded
with `go:embed`. There is no build step and nothing is fetched at run time.

| Route | Response |
| --- | --- |
| `GET /assets/{name}` | The named vendored file, or `404` |

The route serves a fixed list of files rather than a directory, so documentation
and licence files that sit beside them are not reachable through it. Each
response carries `Cache-Control: public, max-age=31536000, immutable` and
`X-Content-Type-Options: nosniff`. The version is part of the file name, so an
upgrade is a new URL rather than a cache to be invalidated. The route requires
no session: a page loads its script before anyone signs in.

`.air.toml` rebuilds on `.go` and `.js` changes, because a vendored script is
embedded and only reaches a running server through a rebuild.

### Content Security Policy

Every rendered page and fragment carries:

```
default-src 'self'; script-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'
```

`script-src 'self'` is why HTMX is vendored: a CDN script is blocked outright.
HTMX needs nothing beyond it. It requests over `XMLHttpRequest` to this origin,
which `default-src 'self'` allows, and the project uses none of the attributes
(`hx-on`, `js:` expressions, event filters) that would require `unsafe-eval`.

### Fragments

A handler may answer one URL with either a whole page or a named block of it.

- A fragment is returned when the request carries `HX-Request: true` and does
  not carry `HX-History-Restore-Request: true`. A history restore replaces the
  whole body, so it is answered with the whole page.
- A response that varies this way carries `Vary: HX-Request`.
- A fragment carries the same `Content-Type` and the same security headers as a
  page.

Controls that HTMX drives keep the `href` or `action` they would have had
without it. A browser that does not run the script follows the link or submits
the form and gets the page; a browser that does gets the fragment and swaps it
in place, which keeps the page's scroll position and the focused field.

The admin map is the first interaction to use this. See
[Map view reference](reference/map-view.md#the-map-region).

## Code boundaries

Command functions assemble dependencies and invoke application behavior. They remain thin and do not implement server or game-engine behavior.

Server and game-engine behavior live in separate packages. Neither belongs in command functions.

Before `github.com/peterbourgon/ff/v4` runs a command, the process loads environment variables through `internal/dotenv`.

## Command configuration

The process entry point reads `ENV` to choose the dotenv environment and defaults to `development`. It loads those files before calling the command runner. The runner then parses command-line flags and environment variables with the `MARAJANDA` prefix; explicit flags take precedence over environment variables. Keeping dotenv initialization outside the runner lets tests and other callers provide isolated environments without the runner overwriting them.

| Flag | Environment variable | Required |
| --- | --- | --- |
| `--root` | `MARAJANDA_ROOT` | Always |
| `--admin-email` | `MARAJANDA_ADMIN_EMAIL` | When creating a persistent database |
| `--admin-secret` | `MARAJANDA_ADMIN_SECRET` | When creating a persistent database |
| `--admin-handle` | `MARAJANDA_ADMIN_HANDLE` | When creating a persistent database |
| `--game-seed` | `MARAJANDA_GAME_SEED` | When creating any database |
| `--width` | `MARAJANDA_WIDTH` | No; defaults to `255` |
| `--height` | `MARAJANDA_HEIGHT` | No; defaults to `127` |
| `--address` | `MARAJANDA_ADDRESS` | No; defaults to `127.0.0.1` |
| `--port` | `MARAJANDA_PORT` | No; defaults to `8443` |
| `--timeout` | `MARAJANDA_TIMEOUT` | No; defaults to `0` (disabled) |

The game seed contains exactly two comma-separated signed 64-bit integers, for example `98374,-98`. Neither integer has a default value.

The width and height are half-extents: the world is `2*width+1` columns by `2*height+1` rows, so the defaults give 511 by 255. They apply only when a database is created and are stored with the game seeds. See [Terrain reference](reference/terrain.md#the-world) for their accepted ranges.

The root value may be `:memory:`. In-memory databases ignore configured admin seed values and use the documented account defaults, but still require `--game-seed`.

## Server lifecycle

The server requires either a persistent database directory or the special database value `:memory:`. See [Datastore](DATASTORE.md) for open and initialization behavior.

A persistent server changes its working directory to the configured root before opening server files. Future request handlers that accept file paths must resolve them within that root and reject traversal or symlink escapes rather than relying on the working directory as a security boundary.

A server using a persistent database supports graceful shutdown and closes the database cleanly.

The server accepts `GET /api/healthz` and returns `204 No Content`. It shuts down gracefully when its context is canceled or its configured non-zero timeout expires.
