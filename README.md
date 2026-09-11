# Marajanda

Marajanda is an open-ended fantasy game played by factions in a shared,
persistent world. Players found a faction, discover a bounded hex world, and
issue turn-based orders to the entities they control. An administrator manages
the game through the Marajanda faction.

The production website will be [marajanda.com](https://marajanda.com).

> [!IMPORTANT]
> Marajanda is beta software. Its rules and interfaces are still evolving, and
> databases created by earlier beta schemas may need to be recreated.

## Run a temporary server

Marajanda requires [Go 1.26](https://go.dev/) or later. From the repository
root, start an in-memory server with:

```sh
go run ./cmd/marajanda --root :memory: --game-seed 98374,-98
```

Open <http://127.0.0.1:8443> and sign in with either temporary account:

| Role | Account | Password |
| --- | --- | --- |
| Admin | `admin@marajanda.com` | `good.luck` |
| Player | `player@marajanda.com` | `good.luck` |

The in-memory world and its accounts disappear when the process stops. See the
[architecture reference](docs/ARCHITECTURE.md#command-configuration) and
[datastore reference](docs/DATASTORE.md#persistent-databases) to configure a
persistent server.

## Documentation

- [Product reference](docs/PRODUCT.md) — roles, factions, turns, and world rules
- [Accounts reference](docs/ACCOUNTS.md) — authentication, invitations, and registration
- [Architecture reference](docs/ARCHITECTURE.md) — stack, configuration, and server lifecycle
- [Datastore reference](docs/DATASTORE.md) — SQLite behavior, migrations, and stored game state
- [REST API v1 reference](docs/reference/api-v1.md) — sessions, resources, and error responses
- [Game reference](docs/reference/glossary.md) — game terminology, with links to detailed rules

## Development

Run the test suite with:

```sh
go test ./...
```

The server renders HTML with HTMX and stores its state in SQLite. Its browser
assets are embedded in the Go binary, so no separate frontend build is needed.

## License

Marajanda is available under the [MIT License](LICENSE).
