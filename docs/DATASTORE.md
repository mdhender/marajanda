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

`current_turn` is the game's clock and the one column of the record that moves. It defaults to `1` when the database is created and is constrained to `1 <= current_turn < 99999999`: a turn starts at 1, only ever increases, and never reaches the end-of-time turn that an unended period runs to. `AdvanceTurn` is the only thing that moves it, and it moves it by one. It processes the orders of the turn it closes in the same transaction; see [Turn processing reference](reference/turn-processing.md).

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

Every account carries `is_active`, an integer constrained to `0` or `1` and
defaulting to `1`. `STRICT` tables have no boolean type, so a constrained value
is an integer with a check. An account written without an opinion is active;
`0` is the value that takes an account away. A deactivated account is not
authenticated. See [Accounts reference](ACCOUNTS.md#deactivation).

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

Each faction is associated with one account. Faction records store the faction name and the faction's race. A faction has no coordinates: it owns entities, and each entity carries its own location.

Configuring a faction founds it. Seating the account, writing the faction record, and creating its founding entities happen in one transaction, so a placement that fails leaves no account seat, no faction and no entity behind. A faction is founded once; reconfiguring it renames its people and does not create a second set of entities.

The main admin's faction follows a separate founding rule. Database creation
writes the main admin account, its fixed `Marajanda` faction, `MARAJANDA-1`, and
its normal founding knowledge in one transaction. Assistant admins control no
faction.

Every faction carries `is_active`, in the same shape and with the same default
as the account column: an integer constrained to `0` or `1`, defaulting to `1`.
A deactivated faction gives no orders, and the store refuses every order write
for one with `ErrFactionInactive`. See
[Orders reference](reference/orders.md#store-methods).

The two flags are independent. Deactivating an account says nothing about its
faction, and deactivating a faction leaves its account able to sign in. A
faction that is configured and inactive is still configured: the flag is not
read as a reason to send a player back to the faction form.

Both flags are set by hand during beta. There is no interface for either.

Race is required and defaults to `human`. It is constrained to `human`, `elf`, `dwarf`, `orc`, `kobold`, and `halfling`. The main admin's faction has the fixed name `Marajanda` and the `human` race. An account that holds an origin but controls no faction, which includes every assistant admin, is treated as `human` by placement.

## Entities

An entity is anything that stands in the world. `entities` holds its identity and its owner:

| Column | Notes |
| --- | --- |
| `id` | Integer primary key. Immutable and never reused. Never a PRNG instance key. |
| `faction_email` | The owning faction. `ON DELETE CASCADE`. Ownership is on the row rather than in a fact because nothing transfers an entity between factions. |
| `created_turn` | The turn the entity was created on. At least 1. |

Everything else about an entity is a fact dated in turns.

### Effective-dated facts

`entity_facts`, `entity_locations`, `entity_allowances`, `units` and `faction_knowledge` are fact tables. Each carries `effective_from` and `effective_through` as turn numbers over the half-open period `[from, through)`, and each row is true on a turn when

```sql
effective_from <= :turn AND :turn < effective_through
```

Both columns are `NOT NULL`. A period that has not ended runs to the end-of-time turn, `99999999`, never to `NULL`. The predicate above is therefore the only one any read needs: no `IS NULL` branch, no `COALESCE`, and no index that behaves differently for an open period than for a closed one. A row missing its end is a constraint violation rather than an open period nobody meant to write, and the check that a period is non-empty — `effective_through > effective_from` — is unconditional.

For one subject, the periods of one fact table are contiguous, never overlap, and exactly one of them runs to the end of time. The subject is the fact's natural key without the period: an entity for most of them, a faction and a hex for `faction_knowledge`. A partial unique index on each fact table holds the last of those: `entity_facts_open`, `entity_locations_open` and `entity_allowances_open` on `entity_id`, `units_open` on `entity_id, kind`, and `faction_knowledge_open` on `faction_email, q, r`.

The end-of-time turn appears in the schema and in `internal/game` as `EndOfTimeTurn`. The schema is built from that constant, so the two cannot drift.

Turn processing closes an open row at `turn + 1` and opens its replacement running from `turn + 1` to the end of time. Founding facts are the exception: they are effective from the turn their faction was configured.

### Fact tables

| Table | Columns | Primary key |
| --- | --- | --- |
| `entity_facts` | `code`, `name`, `kind` | `(entity_id, effective_from)` |
| `entity_locations` | `q`, `r` | `(entity_id, effective_from)` |
| `entity_allowances` | `points` | `(entity_id, effective_from)` |
| `units` | `kind`, `quantity` | `(entity_id, kind, effective_from)` |

`kind` in `entity_facts` is constrained to `leader`, `hamlet`, and `marajanda`. `q, r` in `entity_locations` references `hexes`, so an entity cannot stand on a coordinate the world does not contain. `quantity` in `units` must be positive. `kind` in `units` carries no constraint: the list of unit kinds is a game rule that arrives with the first rule producing one, and nothing seeds inventory yet.

`points` in `entity_allowances` is how many action points the entity has for a turn, and it must be positive. An entity kind that accepts no orders has no allowance, and having none is the absence of a row rather than a zero in one: a hamlet and Marajanda have no row, and a read that finds none reports zero. The value written at creation comes from `game.FoundingAllowance`, so the schema names no number. Nothing changes an allowance yet; the rows are dated anyway, because a rule that read the allowance off the entity's kind would price turn 3 from whatever that kind means today. See [Action points reference](reference/action-points.md#the-allowance).

Every entity fact table cascades from `entities`, which cascades from `factions`, which cascades from `accounts`. `faction_knowledge` cascades from `factions` directly, because it is what the faction knows rather than what one of its entities does.

See [Entities reference](reference/entities.md) for the vocabulary, the code rules, and how state is read as of a turn.

## Knowledge

`faction_knowledge` records what a faction knows about one hex. It is a fact table in the shape above, keyed on the faction and the hex.

| Column | Notes |
| --- | --- |
| `faction_email` | The knowing faction. `ON DELETE CASCADE`. |
| `q`, `r` | The hex. References `hexes`, so a coordinate the world does not contain cannot be written. |
| `state` | `observed` or `explored`. |
| `effective_from`, `effective_through` | The period, as on every fact table. |

There is no `unknown` state. A hex the faction knows nothing about has no row, so nothing is written when a hex stops being unknown and nothing has to be cleaned up.

The foreign key to `hexes` is also the clip. A write names the six hexes around a coordinate and inserts each by selecting it from `hexes`, so a neighbour beyond a pole matches nothing and is dropped rather than failing the write. Columns wrap before the write, so a neighbour at the eastern edge is stored as the canonical hex at the western one.

The rules this table holds — the two states, what writes them, and why the record is the state at the end of a turn — are in [Knowledge reference](reference/knowledge.md).

## Orders

An order is one instruction issued to one entity for one turn, and it is one action. `orders` holds the instruction; each order kind's own detail table holds what that kind needs beyond its kind, one row per order:

| Table | Column | Notes |
| --- | --- | --- |
| `orders` | `turn` | The turn the order was issued for. At least 1. |
| `orders` | `entity_id` | The entity the order is issued to. `ON DELETE CASCADE`. |
| `orders` | `seq` | The order's position in that entity's list for the turn. Contiguous from 1, and constrained to `1 .. 32`. |
| `orders` | `kind` | Constrained to `move` and `rest`. |
| `move_orders` | `turn`, `entity_id`, `seq` | The order the direction belongs to. `ON DELETE CASCADE`. |
| `move_orders` | `direction` | Constrained to `ne`, `e`, `se`, `sw`, `w`, `nw`. |
| `rest_orders` | `turn`, `entity_id`, `seq` | The order the count belongs to. `ON DELETE CASCADE`. |
| `rest_orders` | `count` | How many action points the rest lasts. Constrained to `1 .. 32`. |

Every one of the three has the primary key `(turn, entity_id, seq)`.

An order is issued to an entity rather than to a faction: the faction is reached through the entity. A move goes one way, so `move nw ne e` is three orders rather than one order carrying three directions, and each of the three has its own sequence number and its own price.

A direction is not a nullable column on `orders`. Such a column would also have to mean "not applicable to this order kind", which is what the detail-table pattern exists to avoid: a rest has no direction. A move with no row in `move_orders` is an order a player has added and not yet said the direction of, and the absence of a row is what the blank select on the page means.

A rest is always one row in `rest_orders`, because a rest has no state a player fills in afterwards: it lasts at least one action point, and a `Rest x0` would be an order that costs nothing and does nothing. The bound on the count is the bound on `seq` and carries it for the same reason: it is what keeps a tolerated overspend bounded. What a rest recovers is still [#36](https://github.com/mdhender/marajanda/issues/36).

The last order in an entity's list, when it is a rest, is the trailing Rest the order pre-processor maintains: its count is what the orders before it leave unspent, and the row is deleted rather than written as a zero when they leave nothing. See [Action points reference](reference/action-points.md#the-trailing-rest).

Sequences are contiguous 1..N and every write leaves them that way: removing an order renumbers what follows it, and inserting one shifts what follows it up. An entity's orders therefore have exactly one stored form.

The bound on `seq` is how many orders an entity may carry in a turn. It is what keeps a tolerated overspend bounded rather than a movement allowance, which belongs to turn processing. It is written into the schema from `datastore.MaxOrdersPerEntity`, so the column check and the code that satisfies it read one value, exactly as the end-of-time turn does.

Only the current turn's rows are writable. Every insert, update and delete is refused when the turn is not `game.current_turn`, whatever turn the caller asks for, so advancing the turn is what freezes the turn before it. Nothing deletes an order from a turn the game has moved past. Which order kinds an entity accepts is a game rule in `internal/game` rather than a constraint here; the datastore reads the entity's kind as of the turn and refuses an order that kind does not accept.

Deleting an account erases its orders through the cascade from `entities`. Nothing deletes accounts.

See [Orders reference](reference/orders.md) for the order kinds, the numbering rules, and the pages that write these rows.

## Turn results

Turn processing records what it decided. Orders are the record of intent and these are the record of consequence: they are separate tables sharing the key `(turn, entity_id, seq)`, and nothing ever writes an outcome back onto an order row.

Three tables, because three things of different cardinality happen in a turn. `turn_results` is one row per entity per turn, `turn_result_orders` one row per order, and `turn_result_observations` one row per hex an order revealed.

| Table | Column | Notes |
| --- | --- | --- |
| `turn_results` | `turn`, `entity_id` | Whose turn it was. `ON DELETE CASCADE` from `entities`. |
| `turn_results` | `allowance`, `spent`, `lapsed` | The action point ledger: what the entity had, what its orders were charged, and what nothing reached. |
| `turn_results` | `start_q`, `start_r`, `end_q`, `end_r` | Where the entity stood when the turn opened and where it stopped. Both reference `hexes`. |
| `turn_result_orders` | `turn`, `entity_id`, `seq` | The order this is the outcome of. References both `turn_results` and `orders`, `ON DELETE CASCADE`. |
| `turn_result_orders` | `kind` | Constrained to `move` and `rest`, as `orders.kind` is. |
| `turn_result_orders` | `cost` | What the entity was charged, which is not what the order would have cost. |
| `turn_result_orders` | `carried` | `0` or `1`. STRICT tables have no boolean. |
| `turn_result_orders` | `reason` | `terrain`, `exhaust`, `blocked` or `unknown`, and `NULL` exactly when the order was carried out. |
| `turn_result_orders` | `from_q`, `from_r`, `target_q`, `target_r`, `to_q`, `to_r` | Where the order was resolved from, where a step was aimed, and where it left the entity. |
| `turn_result_observations` | `turn`, `entity_id`, `seq` | The order that revealed the hex. References `turn_result_orders`, `ON DELETE CASCADE`. |
| `turn_result_observations` | `q`, `r` | The hex revealed. References `hexes`. |
| `turn_result_observations` | `state` | `observed` or `explored`, the states of `faction_knowledge`. |

The primary keys are `(turn, entity_id)`, `(turn, entity_id, seq)` and `(turn, entity_id, seq, q, r)`.

A check holds `reason` and `carried` in step, so a carried order cannot also name a failure and a failed one cannot be silent about why.

None of the three coordinate pairs on `turn_result_orders` references `hexes`, because `target` may be a coordinate the world does not have — a step off a pole is exactly that. `turn_result_observations` does reference it, and the insert selects its coordinates from `hexes`, so a ring that runs off a pole records fewer than seven rows. It is the clip `faction_knowledge` takes, and for the same reason.

These are not fact tables. A result belongs to the turn it was recorded on and carries no period: the facts a turn produces are effective from `turn + 1`, and the record of why they exist stays on the turn that produced them.

Nothing deletes a result. They grow every turn for every entity, unlike orders, which exist only where a player acted; retention is not decided. Deleting an account erases them through the cascade from `entities`.

See [Turn results reference](reference/turn-results.md) for the three grains, the failure vocabulary, and the determinism check the record exists for.

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

Creating either kind of database also creates the main admin's active
`Marajanda` faction with the `human` race, `MARAJANDA-1` at the game origin,
and normal founding knowledge of that origin and its six neighbours. The entity
has no action-point allowance because it accepts no orders yet.

When the server creates a new persistent database, it requires `--game-seed`, migrates the database, stores both game seeds and the world's dimensions, generates the world, and seeds the configured default admin account. Those values are normally configured in an environment-specific local dotenv file. Starting with an existing persistent database does not require seed options and does not reseed it.
