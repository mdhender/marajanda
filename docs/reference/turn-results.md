# Turn results reference

What turn processing records: the account of one entity's turn, what each of its
orders did, and what each order revealed.

Implemented by `internal/game` (`executor.go`, `knowledge.go`) and
`internal/datastore` (`result.go`, `executor.go`). See
[#33](https://github.com/mdhender/marajanda/issues/33).

## Vocabulary

| Term | Definition |
| --- | --- |
| Result | What one entity's turn was: its ledger, its order outcomes, and its observations. |
| Ledger | The action points of the turn: the allowance, what was spent, and what lapsed. |
| Order outcome | What one order did: carried out, or failed for a reason. |
| Observation | One hex one order revealed, and the state it was revealed in. |
| Grain | What a record is one row per. There are three here. |

An [order](orders.md) is what a player asked for and a result is what the engine
decided. The two are separate records; see [Orders and results](#orders-and-results).

## The three grains

A result is not one row. Three things of different cardinality happen in a turn,
and each is recorded on its own grain.

| Grain | One row per | Written for |
| --- | --- | --- |
| Ledger | Entity and turn | Every entity of an active faction that stood in the world on the turn |
| Order outcome | Order | Every order the entity carried for the turn |
| Observation | Hex an order revealed | Every hex a step revealed, one order at a time |

A step that lands is one order outcome and up to seven observations: the hex it
entered explored, and the six around it observed. A step that fails on terrain
is one order outcome and one observation, because the exploration happened and
the entity did not. An order that was never afforded is one order outcome and no
observations at all.

Observations are recorded per entity and per order. What the faction ends the
turn *knowing* is a different record, where every entity's sightings collapse
into one state per hex; see [Knowledge reference](knowledge.md).

## The ledger

Per entity per turn, the record accounts for every action point:

| Recorded | Is |
| --- | --- |
| Allowance | What the entity had. The [fact](entities.md#effective-dating) effective on the turn. |
| Spent | What its orders were charged. |
| Lapsed | What nothing reached. |
| Start | Where the entity stood when the turn opened. |
| End | Where it stopped. |

`Spent` and `Lapsed` always sum to `Allowance`. Order entry tolerates a list
that costs more than the entity can afford, but an order it cannot afford is not
carried out and charges nothing, so a turn never spends past its allowance. The
charges that make up `Spent` are on the order outcomes, one per order.

An entity that was given nothing to do has a ledger. Its whole allowance lapsed,
which is a line a player asks about rather than a missing row to infer it from.
An entity kind that takes no orders has one too, accounting for nothing: a
hamlet's turn is a row of zeroes until something gives a settlement outcomes to
record.

Where a turn's points went is [Action points reference](action-points.md); this
document records the account, not the rule.

## Order outcomes

One row per order, in sequence order, carrying what the order did.

| Recorded | Is |
| --- | --- |
| Kind | What the order told the entity to do. |
| Cost | What the entity was charged for it. |
| Carried | Whether the order happened. |
| Reason | Why it did not. Empty when it was carried out. |
| From | Where the entity stood when the order was resolved. |
| Target | Where a step was aimed. `From` for an order that aims nowhere. |
| To | Where the order left the entity. `From` for everything that did not move it. |

`Cost` is what was charged rather than what the order would have cost. An order
the entity could not afford charges nothing; a step that fails on terrain is
charged in full.

`From` is not where the entity stood when the turn opened. A step that fails does
not move it, so the order after it resolves from the hex it did not leave, and a
report needs all three coordinates to explain a plan that went sideways at step
two.

### The failure vocabulary

| Reason | Means |
| --- | --- |
| `terrain` | The destination cannot be entered: impassable ground, or a coordinate the world does not have. |
| `exhaust` | The entity could not afford the order. Every order from the first exhaustion carries it. |
| `blocked` | Something in the destination prevented entry. Reserved; nothing blocks a hex yet. |
| `unknown` | The order does not say what to do: a move a player added and never gave a direction. |

`blocked` is in the vocabulary and is not produced. There is no stacking rule and
no zone of control, so the word arrives with the rule that earns it.

A move partially succeeds. The orders before a failure stand, the entity stops
where it stopped, and each remaining order carries its own reason; an illegal
step does not void the rest of the list.

## Observations

One row per hex an order revealed, carrying the order that revealed it and the
state it was revealed in — `observed` or `explored`, the two states of the
[knowledge record](knowledge.md#the-two-states).

| Order | Reveals |
| --- | --- |
| A step that landed | The hex entered explored, its six neighbours observed |
| A step that failed on terrain | The hex it walked into observed |
| Anything else | Nothing |

A hex the world does not have is not recorded. The world is its own filter, so a
ring that runs off a pole records fewer than seven rows. Columns wrap, so a ring
at the eastern edge records hexes at the western one.

One hex may be revealed by two orders of the same turn — a step observes what a
later step explores — and both rows are kept. They are two sightings, and the
record says which order made each.

What an observation of another faction's entity contains is not decided;
see [#38](https://github.com/mdhender/marajanda/issues/38).

## Orders and results

Orders are the record of intent and results are the record of consequence. They
are separate tables sharing the key `(turn, entity_id, seq)`.

Nothing writes an outcome back onto an order row. Orders freeze when the turn
advances, and a result column on an order would make that rule read "orders
freeze except for these columns". Keeping them apart is also what makes the
determinism check below possible.

The order key is optional, not fundamental. The result of a turn is keyed on the
entity and the turn; an outcome that has an order to hang from carries that
order's sequence number, and an outcome that has none — an allowance that
lapsed, and the settlement outcomes that arrive with the first rule producing
one — does not.

## Per faction

Results are recorded per faction, through the entity that acted. Nothing yet
produces an event two factions can see: entities do not interact, nothing blocks,
and an observation of a rival is one-sided by construction. Whether results
become per-faction views of shared events is decided by the first two-sided
event, not before it.

## What is not recorded

- **Reports.** What a player is shown is downstream of this record and is not
  built yet.
- **Sightings of contents.** An observation records the hex and its state.
  What was standing in it is [#38](https://github.com/mdhender/marajanda/issues/38).
- **Retention.** Results grow every turn for every entity, unlike orders, which
  exist only where a player acted. Nothing compacts or deletes them.

## Storage

| Table | Columns | Primary key |
| --- | --- | --- |
| `turn_results` | `allowance`, `spent`, `lapsed`, `start_q`, `start_r`, `end_q`, `end_r` | `(turn, entity_id)` |
| `turn_result_orders` | `kind`, `cost`, `carried`, `reason`, `from_q`, `from_r`, `target_q`, `target_r`, `to_q`, `to_r` | `(turn, entity_id, seq)` |
| `turn_result_observations` | `state` | `(turn, entity_id, seq, q, r)` |

These are not fact tables. A result is of the turn it was recorded on and is
never dated forward: the facts a turn produces are effective from `turn + 1`, and
the record of why they exist stays on the turn that produced them.

Column definitions are in [Datastore](../DATASTORE.md#turn-results).

## Store methods

| Method | Returns |
| --- | --- |
| `ResultsAsOf(ctx, email, turn)` | What the faction's entities did on `turn`, by entity in creation order. |

It refuses a turn the game cannot be on, and an account that controls no faction
with `ErrUnknownFaction`. A turn that has not been processed answers with
nothing.

Results are read rather than derived again. The derivation is the replay, and a
record that is only ever derived is nothing to compare a replay against.

## Determinism

Processing reads the stored orders, the seeds, and facts dated at or before the
turn. It draws on no roller, so a replay of the same rows records the same
results: create a database with the same seeds and dimensions, insert the same
orders, advance, and compare every row of all three grains.

That comparison is a golden test over the whole rules engine, in the way
`internal/prng/testdata/golden.json` is one over the PRNG. It is possible because
the record of what happened is stored beside the record of what was asked for
rather than folded into it. See `internal/prng/doc.go` and
[Turn processing reference](turn-processing.md#determinism).
