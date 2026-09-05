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
- During beta, migrations are a squashed baseline. Databases created by an earlier beta schema are unsupported and must be deleted and recreated.

## Game

The database contains exactly one game record. It stores two required signed 64-bit integer seeds used to initialize the game's deterministic PRNG, and the required world radius. The seeds have no default values. The radius defaults to `30` when the database is created and must be between `20` and `120`. None of the three change when the database is reopened: the stored world was generated from all three and would no longer match if any of them did.

## Accounts

Every account stores a required origin hex as axial `q` and `r` components and
a required map rotation from `0` through `5`. Origin coordinates are unique
across accounts. The origin and rotation are assigned when the account is
created and do not belong to its faction.

The main admin account has the game origin `(0, 0, 0)` and rotation `0`. Every
later account, including an assistant admin, uses the deterministic placement
and rotation rules in [Player origin reference](reference/player-origin.md).

## Hexes

Each map hex stores axial `q` and `r` coordinates as its composite primary key,
a required terrain type, and a required elevation in metres.

The table holds the whole world. Every hex within the world radius of the game
origin is generated and inserted in one transaction when the database is
created, in the same call that inserts the game record. No hexes are added or
changed afterwards.

Account origins reference hexes, so an account cannot be created on a
coordinate the world does not contain. Creating an account inserts only the
account: its origin hex already exists.

See [Terrain reference](reference/terrain.md) for the terrain values, elevation,
and generation rules, and [Map view reference](reference/map-view.md) for how a
map is drawn from them.

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

When `:memory:` is selected, `--game-seed` is required. The server creates and migrates an in-memory database, stores both game seeds and the world radius, generates the world, and seeds:

| Role | Email | Password |
| --- | --- | --- |
| Admin | `admin@marajanda.com` | `good.luck` |
| Player | `player@marajanda.com` | `good.luck` |

The corresponding default handles are `admin` and `player`.

These are intentional credentials for temporary server instances and do not produce warnings or errors.

When the server creates a new persistent database, it requires `--game-seed`, migrates the database, stores both game seeds and the world radius, generates the world, and seeds the configured default admin account. Those values are normally configured in an environment-specific local dotenv file. Starting with an existing persistent database does not require seed options and does not reseed it.
