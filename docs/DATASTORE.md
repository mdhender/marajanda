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

The database contains exactly one game record. It stores two required signed 64-bit integer seeds used to initialize the game's deterministic PRNG, the world's required `width` and `height`, and the current turn. The seeds have no default values. The dimensions are half-extents: the world is `2*width+1` columns by `2*height+1` rows. `width` defaults to `255` when the database is created and must be between `20` and `511`; `height` defaults to `127` and must be between `20` and `255`. None of the four change when the database is reopened: the stored world was generated from all four and would no longer match if any of them did.

`current_turn` is the game's clock and the one column of the record that moves. It defaults to `1` when the database is created and is constrained to `1 <= current_turn < 99999999`: a turn starts at 1, only ever increases, and never reaches the end-of-time turn that an unended period runs to. `AdvanceTurn` is the only thing that moves it, and it moves it by one.

## Accounts

Every account stores an optional origin hex as axial `q` and `r` components. The
two origin columns are `NULL` together or not at all. Origin coordinates are
unique across accounts; SQLite treats `NULL`s as distinct in a `UNIQUE`
constraint, so any number of accounts without an origin coexist while the
constraint still rejects two accounts on one hex.

The origin is a permanent founding seat, not a current position. It is where a
faction's founding entities are placed and what the placement exclusion set is
built from; where the faction's entities stand now is a fact of each entity.

The origin columns are nullable because placement needs a faction's race, which
is not known when a player's account row is written:

| Account | Origin assigned |
| --- | --- |
| Main admin | When the database is created, as the game origin `(0, 0, 0)` |
| Any other admin | When the account is created |
| Player | When the account configures its faction |

A player account therefore has no origin between its creation and its faction
configuration. The seat, the faction and the faction's founding entities are
written in one transaction, so a placement that fails leaves none of them
behind.

The main admin account has the game origin `(0, 0, 0)`. Every later account,
including an assistant admin, uses the deterministic placement rules in
[Player origin reference](reference/player-origin.md).

## Hexes

Each map hex stores axial `q` and `r` coordinates as its composite primary key,
a required terrain type, and a required elevation in metres.

The table holds the whole world. Every hex of the world is generated and
inserted in one transaction when the database is created, in the same call that
inserts the game record. No hexes are added or changed afterwards.

Account origins reference hexes, so an account cannot be seated on a
coordinate the world does not contain. Seating an account inserts no hex: its
origin hex already exists.

See [Terrain reference](reference/terrain.md) for the terrain values, elevation,
and generation rules, and [Map view reference](reference/map-view.md) for how a
map is drawn from them.

## Factions

Each player faction is associated with one account. Faction records store the faction name and the faction's race. A faction has no coordinates: it owns entities, and each entity carries its own location.

Configuring a faction founds it. Seating the account, writing the faction record, and creating its founding entities happen in one transaction, so a placement that fails leaves no account seat, no faction and no entity behind. A faction is founded once; reconfiguring it renames its people and does not create a second set of entities.

Race is required and defaults to `human`. It is constrained to `human`, `elf`, `dwarf`, `orc`, `kobold`, and `halfling`. An account that holds an origin but controls no faction, which includes every admin, is treated as `human` by placement.

## Entities

An entity is anything that stands in the world. `entities` holds its identity and its owner:

| Column | Notes |
| --- | --- |
| `id` | Integer primary key. Immutable and never reused. Never a PRNG instance key. |
| `faction_email` | The owning faction. `ON DELETE CASCADE`. Ownership is on the row rather than in a fact because nothing transfers an entity between factions. |
| `created_turn` | The turn the entity was created on. At least 1. |

Everything else about an entity is a fact dated in turns.

### Effective-dated facts

`entity_facts`, `entity_locations`, and `units` are fact tables. Each carries `effective_from` and `effective_through` as turn numbers over the half-open period `[from, through)`, and each row is true on a turn when

```sql
effective_from <= :turn AND :turn < effective_through
```

Both columns are `NOT NULL`. A period that has not ended runs to the end-of-time turn, `99999999`, never to `NULL`. The predicate above is therefore the only one any read needs: no `IS NULL` branch, no `COALESCE`, and no index that behaves differently for an open period than for a closed one. A row missing its end is a constraint violation rather than an open period nobody meant to write, and the check that a period is non-empty — `effective_through > effective_from` — is unconditional.

For one entity, the periods of one fact table are contiguous, never overlap, and exactly one of them runs to the end of time. A partial unique index on each fact table holds the last of those: `entity_facts_open` and `entity_locations_open` on `entity_id`, and `units_open` on `entity_id, kind`.

The end-of-time turn appears in the schema and in `internal/game` as `EndOfTimeTurn`. The schema is built from that constant, so the two cannot drift.

Turn processing closes an open row at `turn + 1` and opens its replacement running from `turn + 1` to the end of time. Founding facts are the exception: they are effective from the turn their faction was configured.

### Fact tables

| Table | Columns | Primary key |
| --- | --- | --- |
| `entity_facts` | `code`, `name`, `kind` | `(entity_id, effective_from)` |
| `entity_locations` | `q`, `r` | `(entity_id, effective_from)` |
| `units` | `kind`, `quantity` | `(entity_id, kind, effective_from)` |

`kind` in `entity_facts` is constrained to `leader` and `hamlet`. `q, r` in `entity_locations` references `hexes`, so an entity cannot stand on a coordinate the world does not contain. `quantity` in `units` must be positive. `kind` in `units` carries no constraint: the list of unit kinds is a game rule that arrives with the first rule producing one, and nothing seeds inventory yet.

Every fact table cascades from `entities`, which cascades from `factions`, which cascades from `accounts`.

See [Entities reference](reference/entities.md) for the vocabulary, the code rules, and how state is read as of a turn.

## Orders

An order is one instruction issued to one entity for one turn. `orders` holds the instruction and `order_steps` holds a move's directions:

| Table | Column | Notes |
| --- | --- | --- |
| `orders` | `turn` | The turn the order was issued for. At least 1. |
| `orders` | `entity_id` | The entity the order is issued to. `ON DELETE CASCADE`. |
| `orders` | `seq` | The order's position in that entity's list for the turn. Contiguous from 1. |
| `orders` | `kind` | Constrained to `move`. |
| `order_steps` | `turn`, `entity_id`, `seq` | The order the step belongs to. `ON DELETE CASCADE`. |
| `order_steps` | `step` | The step's position in the order. Contiguous from 1, and constrained to `1 .. 32`. |
| `order_steps` | `direction` | Constrained to `ne`, `e`, `se`, `sw`, `w`, `nw`. |

The primary keys are `(turn, entity_id, seq)` and `(turn, entity_id, seq, step)`.

An order is issued to an entity rather than to a faction: the faction is reached through the entity. A move's directions are a list, so `move nw ne e` is three rows and a blank box is the absence of a row rather than a NULL in a column that would also have to mean "not applicable to this order kind". Sequences and steps are compacted on every write, so one order has exactly one stored form.

The bound on `step` is a storage sanity limit rather than a game rule; the movement allowance belongs to turn processing. It is written into the schema from `datastore.MaxOrderSteps`, so the column check and the code that satisfies it read one value, exactly as the end-of-time turn does.

Only the current turn's rows are writable. Every insert, update and delete is refused when the turn is not `game.current_turn`, whatever turn the caller asks for, so advancing the turn is what freezes the turn before it. Nothing deletes an order from a turn the game has moved past. Which order kinds an entity accepts is a game rule in `internal/game` rather than a constraint here; the datastore reads the entity's kind as of the turn and refuses an order that kind does not accept.

Deleting an account erases its orders through the cascade from `entities`. Nothing deletes accounts.

See [Orders reference](reference/orders.md) for the order kinds, the numbering rules, and the pages that write these rows.

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

When `:memory:` is selected, `--game-seed` is required. The server creates and migrates an in-memory database, stores both game seeds and the world's dimensions, generates the world, and seeds:

| Role | Email | Password |
| --- | --- | --- |
| Admin | `admin@marajanda.com` | `good.luck` |
| Player | `player@marajanda.com` | `good.luck` |

The corresponding default handles are `admin` and `player`.

These are intentional credentials for temporary server instances and do not produce warnings or errors.

When the server creates a new persistent database, it requires `--game-seed`, migrates the database, stores both game seeds and the world's dimensions, generates the world, and seeds the configured default admin account. Those values are normally configured in an environment-specific local dotenv file. Starting with an existing persistent database does not require seed options and does not reseed it.
