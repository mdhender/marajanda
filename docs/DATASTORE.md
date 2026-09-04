# Datastore Reference

Marajanda uses ZombieZen SQLite for persistent and in-memory data.

## Database identity and connection settings

- SQLite `application_id` is the ASCII encoding of `MRJ0`: hexadecimal `0x4D524A30`, decimal `1297238576`.
- Every persistent connection enables write-ahead logging (WAL).
- Every connection enables foreign-key enforcement, including test and in-memory connections.
- SQLite does not support WAL for true in-memory databases; those databases use SQLite's `memory` journal mode.

## Migrations

- Every open operation migrates up.
- Migrate down is not supported.
- Schema versions use ZombieZen's SQL migration package. Marajanda does not maintain a custom migration-version table or mechanism.
- Open fails when the database schema version is newer than the version the application supports.

## Factions

Each player faction is associated with one account. Faction records store the faction name and its current axial `q` and `r` map coordinates. New faction records start at `(0, 0)`.

## Open modes

The datastore exposes distinct open operations for:

1. A non-shared in-memory database. ZombieZen creates a unique database instance for this operation.
2. A named, shared in-memory database. This is separate from the non-shared operation.
3. A persistent database rooted at a caller-supplied directory.

In-memory databases are preferred for tests when practical. Tests cover both in-memory modes where connection-sharing behavior matters.

## Persistent databases

The persistent open operation accepts a directory path, not a database-file path. It always uses the filename `marajanda.db` within that directory.

The supplied directory must already exist and must be a directory. Open never creates it or any missing parent directory. A missing or invalid directory is a hard failure.

The server may create and migrate `marajanda.db` when the file does not exist.

## Initial data

When `:memory:` is selected, the server creates and migrates an in-memory database and seeds:

| Role | Email | Password |
| --- | --- | --- |
| Admin | `admin@marajanda.com` | `good.luck` |
| Player | `player@marajanda.com` | `good.luck` |

The corresponding default handles are `admin` and `player`.

These are intentional credentials for temporary server instances and do not produce warnings or errors.

When the server creates a new persistent database, it migrates the database and seeds the configured default admin account. That account is normally configured in an environment-specific local dotenv file. Starting with an existing persistent database does not reseed it.
