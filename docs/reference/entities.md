# Entities reference

What a faction owns, what stands on the map, and how a fact about it is dated.

Implemented by `internal/game` (`entity.go`, `turn.go`) and
`internal/datastore` (`entity.go`). See
[#32](https://github.com/mdhender/marajanda/issues/32).

## Vocabulary

| Term | Definition |
| --- | --- |
| Faction | What a player controls. It owns entities and has no location of its own. |
| Entity | Anything that stands in the world. It has a location, a code, a name, and a kind. Orders are issued to entities. |
| Unit | Inventory: a quantity of a kind held by an entity. It has no code, no name, and no identity of its own. |
| Fact | One row of a fact table, true of its entity over a period of turns. |

A settlement is an entity, not a unit. It has every property an entity has: a
location, a mutable kind, a name a player may change, and inventory of its own.
What separates a hamlet from a leader is which orders reach it.

## Entity kinds

| Kind | Code prefix |
| --- | --- |
| `leader` | `LEADER-` |
| `hamlet` | `HAMLET-` |

A player's faction is founded with two entities on its origin hex, in this
order: `LEADER-1` and `HAMLET-1`. Founding happens in the same transaction that
seats the account and writes the faction; see
[Datastore](../DATASTORE.md#factions).

Which order kinds an entity kind accepts is a game rule: a leader accepts
`move`, and a hamlet accepts nothing. See
[Orders reference](orders.md#order-kinds).

## Identity, code, name, kind

| Property | Mutable | Where it lives |
| --- | --- | --- |
| Identity | No. An integer, never reused. | `entities.id` |
| Code | No. Frozen at creation. | `entity_facts.code` |
| Name | Yes, through an order. | `entity_facts.name` |
| Kind | Yes. | `entity_facts.kind` |
| Location | Yes. | `entity_locations` |
| Ownership | No. Nothing transfers an entity between factions. | `entities.faction_email` |

A code is a kind and a per-faction sequence for that kind: `LEADER-1`,
`HAMLET-1`. The sequence counts codes the faction has already spent, not
entities that currently hold a kind, so a hamlet that changes kind still holds
hamlet number one and the faction's next hamlet is `HAMLET-2`. Two factions each
have a `LEADER-1`.

A name defaults to its entity's code.

An entity's identity is never a PRNG instance key. A rule that needs per-entity
randomness keys on values recorded in history — the faction, the creation turn,
the code. See `internal/prng/doc.go`.

## Turns

A turn is an integer. It starts at 1, only ever increases, and there is one
current turn per database, held in `game.current_turn`. The admin dashboard's
turn control advances it; see [Orders reference](orders.md#routes).

| Constant | Value | Meaning |
| --- | --- | --- |
| `game.StartOfTimeTurn` | `0` | Already true before the game's first turn. |
| `game.FirstTurn` | `1` | The turn a new database sits on. |
| `game.EndOfTimeTurn` | `99999999` | Still true; the period has not ended. |

A turn the game can be on satisfies `1 <= turn < EndOfTimeTurn`, which
`game.ValidTurn` reports. Neither sentinel is such a turn.

## Effective dating

Every fact carries `effective_from` and `effective_through` as turn numbers over
the half-open period `[from, through)`. Both columns are `NOT NULL`. A fact is
true on a turn when

```sql
effective_from <= :turn AND :turn < effective_through
```

A period that has not ended runs to `EndOfTimeTurn`, never to `NULL`. There is
no `IS NULL` branch, no `COALESCE`, and no index that behaves differently for an
open period than for a closed one. A period missing its end is a constraint
violation.

For one entity, the periods of one fact table are contiguous, never overlap, and
exactly one of them runs to `EndOfTimeTurn`. A partial unique index holds the
last of those.

The clock is the turn, and turn processing is what moves it. An order issued
during turn 3 executes when turn 3 is processed, and the fact it writes is
effective from turn 4: on turn 3 the settlement is still named Mudville, and
every turn-3 report line says Mudville. Turn processing closes an open row at
`turn + 1` and opens its replacement running from `turn + 1` to `EndOfTimeTurn`.

A faction configured during turn `T` is the exception. Its entities' founding
facts are effective from `T`, because nothing about them waits on a turn to be
processed.

These rows are the single source of truth, not a snapshot alongside one. "Where
was `LEADER-1` on turn 7" is a query, and it agrees with a replay of the orders
because both describe the same rows.

## Fact tables

| Table | Holds | Grain |
| --- | --- | --- |
| `entity_facts` | `code`, `name`, `kind` | One row per entity per period |
| `entity_locations` | `q`, `r` | One row per entity per period |
| `units` | `kind`, `quantity` | One row per entity per unit kind per period |

Location is its own table because it is the attribute that changes every turn a
leader moves. Code, name and kind share one table because they change rarely and
together. As other attributes prove volatile they get their own fact tables.

A unit's `kind` carries no constraint. The list of unit kinds is a game rule,
and it arrives with the first rule that produces one. Nothing creates, consumes
or moves inventory yet, so no faction is seeded with any.

Column definitions are in [Datastore](../DATASTORE.md#entities).

## Reading state

```sql
SELECT code, name, kind FROM entity_facts
WHERE entity_id = ?1
  AND effective_from <= ?2
  AND ?2 < effective_through;
```

`datastore.Store` exposes:

| Method | Returns |
| --- | --- |
| `CurrentTurn(ctx)` | The turn the game is on. |
| `AdvanceTurn(ctx)` | Moves the clock on by one and returns the new turn. |
| `EntitiesAsOf(ctx, email, turn)` | A faction's entities as they stood on `turn`, in creation order. |

`EntitiesAsOf` rejects a number that is not a turn the game can be on. Its joins
are inner: an entity with no fact covering the turn did not stand in the world
on that turn and is omitted.

The player dashboard reads the current turn and then reads entities as of that
turn, and reports both.

## What this is not

Visibility does not come from entity locations yet. `VisibleHexes` still returns
an account's origin hex alone. See
[Player origin reference](player-origin.md) and
[Map view reference](map-view.md).
