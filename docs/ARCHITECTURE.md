# Architecture Reference

## Application stack

- Go implements the application.
- HTMX provides server-rendered web interactions.
- Alpine.js may provide client-side behavior that HTMX cannot reasonably supply; it is not required by default.
- ZombieZen SQLite provides persistence and schema migrations.

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
| `--address` | `MARAJANDA_ADDRESS` | No; defaults to `127.0.0.1` |
| `--port` | `MARAJANDA_PORT` | No; defaults to `8443` |
| `--timeout` | `MARAJANDA_TIMEOUT` | No; defaults to `0` (disabled) |

The game seed contains exactly two comma-separated signed 64-bit integers, for example `98374,-98`. Neither integer has a default value.

The root value may be `:memory:`. In-memory databases ignore configured admin seed values and use the documented account defaults, but still require `--game-seed`.

## Server lifecycle

The server requires either a persistent database directory or the special database value `:memory:`. See [Datastore](DATASTORE.md) for open and initialization behavior.

A persistent server changes its working directory to the configured root before opening server files. Future request handlers that accept file paths must resolve them within that root and reject traversal or symlink escapes rather than relying on the working directory as a security boundary.

A server using a persistent database supports graceful shutdown and closes the database cleanly.

The server accepts `GET /api/healthz` and returns `204 No Content`. It shuts down gracefully when its context is canceled or its configured non-zero timeout expires.
