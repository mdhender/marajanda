# Turn processing reference

What happens when the admin advances the turn: which orders are carried out,
what they write, and what is left alone.

Implemented by `internal/game` (`executor.go`) and `internal/datastore`
(`executor.go`, `order.go`). See
[#28](https://github.com/mdhender/marajanda/issues/28).

## Vocabulary

| Term | Definition |
| --- | --- |
| Closed turn | The turn the game was on when processing ran. Its orders are the ones carried out. |
| Executor | What walks an entity's orders and decides what happened. |
| Outcome | What one order did: carried out, or failed for a reason. |
| Carried | Of an order: it happened, and it was charged. |
| Lapsed | Action points the entity's orders did not reach. |

The [action point](action-points.md) rules decide what an order costs and what
an entity may spend. This document describes what processing does with them.

## What advancing the turn does

`POST /admin/turn` calls `AdvanceTurn`, which in one transaction:

1. Reads every active faction, by email.
2. For each, reads the knowledge effective on the closed turn, its entities as
   they stood on that turn, and their orders for it.
3. Walks each entity's orders against its allowance, resolving every step from
   where the entity actually stands.
4. Writes what the turn produced — locations and knowledge — effective from
   `turn + 1`.
5. Increments `game.current_turn`.

A turn is processed whole or not at all. Every write is in the one transaction
that moves the clock, so a failure anywhere leaves the game on the turn it was
on with nothing applied.

## What is processed

| Subject | Processed |
| --- | --- |
| An active faction's entity carrying orders | Yes |
| An entity carrying no orders | No. It does nothing, reveals nothing, and its whole allowance lapses. |
| An entity that did not stand in the world on the turn | No. It is not read. |
| A deactivated faction | No. See below. |

### Inactive factions

A deactivated faction gives no orders, so the orders it wrote while it was
active are history rather than instructions. They stay exactly as they were
written, nothing carries them out, and its entities neither move nor learn.
Reactivating a faction does not carry out the orders of a turn that has closed.

The account flag is a different flag and processing does not read it: a faction
whose player cannot sign in still acts. See
[Accounts reference](../ACCOUNTS.md#deactivation).

## How an entity's orders are resolved

Orders are resolved in sequence order, from where the entity actually stands. A
step that fails does not move it, so the next order is resolved from the hex it
did not leave.

An entity is walked until it cannot afford its next order. The orders it could
afford stand, it stops where it stopped, and every order from the crossing on is
exhausted — including one later in the list that would have cost less.

Costs are read from the knowledge effective when the turn opened, and the world
as it is. Both engines price through `game.Price`; the executor is the caller
that hands it a ground-truth `game.Sight`, so a step into ground that cannot be
entered is seen as failing rather than assumed to land. See
[Action points reference](action-points.md#the-two-engines).

## Outcomes

Every order produces one outcome. An order that was carried out has no failure
reason; one that was not names why.

| Outcome | Charged | Effect |
| --- | --- | --- |
| Carried | The order's cost | A move puts the entity in the hex; a rest does nothing yet ([#36](https://github.com/mdhender/marajanda/issues/36)). |
| `terrain` | The step's cost | No movement. The hex the step walked into becomes known. |
| `exhaust` | Nothing | No movement. The entity could not afford the order. |
| `unknown` | Nothing | No movement. The order does not say what to do: a move a player added and never gave a direction. |

`blocked` is in the vocabulary and is not produced. Nothing blocks a hex yet:
there is no stacking rule and no zone of control.

A step into a coordinate the world does not have fails for `terrain`. Rows do
not wrap, so a step off a pole names a row the world has no hex in, and the
world is its own filter.

Action points the orders did not reach lapse. Processing appends nothing to an
entity's list, so a leader that was given four points of orders out of six
spends four and loses two. See
[Action points reference](action-points.md#the-trailing-rest).

## What it writes

| Record | Written when | Effective from |
| --- | --- | --- |
| `entity_locations` | The entity ended the turn somewhere other than where it started | `turn + 1` |
| `faction_knowledge` | An entity entered a hex: that hex explored, its six neighbours observed | `turn + 1` |
| `faction_knowledge` | A step failed on terrain: the hex it walked into observed | `turn + 1` |
| `game.current_turn` | Always | Immediately |

Nothing is updated in place. A location fact is closed at `turn + 1` and its
replacement opened there, so the row that said where the entity was during the
turn stays and a turn-N report reads a turn-N location. See
[Entities reference](entities.md#effective-dating).

An entity that ended where it started keeps the location fact it had. There is
nothing to date: the fact did not change.

## What it does not write

- **Orders.** The executor writes no orders. The orders of the closed turn are
  the history a replay reads, and nothing rewrites them — not a result, not a
  trailing Rest. See [Orders reference](orders.md#history).
- **Allowances.** An allowance is not a balance. Nothing carries into the next
  turn, so no total is written back.
- **Results.** What happened is computed and applied; it is not yet stored.
  [#33](https://github.com/mdhender/marajanda/issues/33) owns the record and
  the reports that read it.

## Determinism

Processing reads the stored orders and facts dated at or before the closed turn,
and nothing else. It draws on no roller: the movement rule introduces no
randomness, so it needs no domain tag.

Knowledge is frozen at the turn boundary, so no entity's march changes what
another entity's march costs. The result of a turn therefore does not depend on
the order entities are walked in. Processing walks factions by email and
entities in creation order anyway, so two runs write the same rows in the same
order.

A replay is: create a database with the same seeds and dimensions, insert the
same orders for turn 1, advance, and compare. See `internal/prng/doc.go` and
[Orders reference](orders.md#history).

## Store methods

| Method | Effect |
| --- | --- |
| `AdvanceTurn(ctx)` | Processes the current turn's orders, moves the clock on by one, and returns the new turn. |

## Routes

| Route | Role | Response |
| --- | --- | --- |
| `POST /admin/turn` | `admin` | Processes the turn, advances it, and returns to the admin dashboard |

The route reports nothing about what the turn did. What a player is told
arrives with [#33](https://github.com/mdhender/marajanda/issues/33).
