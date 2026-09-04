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

## Server lifecycle

The server requires either a persistent database directory or the special database value `:memory:`. See [Datastore](DATASTORE.md) for open and initialization behavior.

A server using a persistent database supports graceful shutdown and closes the database cleanly.
