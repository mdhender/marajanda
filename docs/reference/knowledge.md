# Knowledge reference

What a faction knows about the world, how it learns, and how a rule reads it as
of a turn.

Implemented by `internal/game` (`knowledge.go`) and `internal/datastore`
(`knowledge.go`). See
[#38](https://github.com/mdhender/marajanda/issues/38).

## Vocabulary

| Term | Definition |
| --- | --- |
| Observed | The faction knows the hex's terrain attributes: its type, its elevation, and whether it can be entered. Nothing about its contents. |
| Explored | The faction knows the above, and what was standing in the hex when its entity was there. |
| Known | Either state. A hex the faction has not observed is unknown. |

Knowledge belongs to a faction, not to an entity. It is what the faction knows,
however many of its entities did the learning.

## The two states

The states are ordered and monotone: unknown becomes observed, observed becomes
explored, and nothing goes backwards. A faction does not forget terrain.

Unknown is not a third state. It is the absence of a record, so nothing is ever
stored to say a hex is unknown and nothing has to be written when a hex stops
being one.

An entity standing in a hex that only saw a neighbour does not write that
neighbour's hex back down: a sighting of ground the faction has walked leaves
`explored` alone.

## What a turn records

The record is the state **at the end of a turn**. Nothing stores the middle of
one.

A faction may walk several entities through one hex in a turn, and it may
observe a hex from one entity and explore it with another. All of it collapses
into one outcome for that hex on that turn. Two consequences:

- **A hex that goes from unknown straight to explored in one turn leaves one
  period, not two.** The intermediate state was never true at a turn boundary,
  so it is never stored.
- **Processing is order-independent.** Walking a turn's entities in a different
  order reaches the same rows, so a replay does not have to reproduce the order
  they were walked in — only which turn they moved on.

## What writes it

Only founding and turn processing. Nothing else may write knowledge, or a replay
from the stored orders and the seeds would not reproduce it.

| Event | Writes | Effective from |
| --- | --- | --- |
| Founding | The origin hex explored, its six neighbours observed | The founding turn |
| An entity enters a hex | That hex explored, its six neighbours observed | `turn + 1` |

Founding is the same exception the founding entity facts take: nothing about it
is waiting on a turn to be processed. So a faction opens turn 1 knowing seven
hexes. See [Entities reference](entities.md#effective-dating).

A step that fails because the ground turned out to be impassable still marks the
hex. The exploration happened; the entity is simply standing where it started.
See [Action points reference](action-points.md#impassable-destinations).

A neighbour that is not a hex of the world is not recorded. The world is its own
filter, so a ring that runs off a pole writes fewer than six hexes rather than
naming a row the world does not have. Columns wrap, so a ring at the eastern
edge names hexes at the western one; see [Compass reference](compass.md).

## What reads it

- **Movement cost.** A step onto ground the faction knew when the turn opened
  costs less than a step onto ground it did not, so this is a rules input rather
  than a rendering detail. See [Action points reference](action-points.md).
- **The player map.** `VisibleHexes` is a read of this record. If the map derived
  visibility one way and the cost rule another, the map would lie about what a
  move will cost. See [Map view reference](map-view.md#visible-hexes).

The two states do not yet draw differently. What separates an explored hex from
an observed one is what was standing in it, and nothing records sightings yet,
so the map draws terrain for both. The distinction is in the record, waiting for
something to show.

## Sightings

What an explored hex holds — a rival's entities, a settlement, whatever else a
hex may contain — is not recorded yet, and neither is what an observation of
another faction's entity contains. That arrives with the turn results that read
it, [#33](https://github.com/mdhender/marajanda/issues/33).

## Storage

| Table | Columns | Primary key |
| --- | --- | --- |
| `faction_knowledge` | `state` | `(faction_email, q, r, effective_from)` |

A fact table like every other: `effective_from` and `effective_through` as turn
numbers over the half-open period `[from, through)`, read with the one predicate

```sql
effective_from <= :turn AND :turn < effective_through
```

For one faction and one hex the periods are contiguous, never overlap, and
exactly one runs to `EndOfTimeTurn`. `faction_knowledge_open` holds the last of
those.

Column definitions are in [Datastore](../DATASTORE.md#knowledge).

## Store methods

| Method | Returns |
| --- | --- |
| `KnowledgeAsOf(ctx, email, turn)` | The faction's `game.KnowledgeSet` on `turn`. |
| `MarkEntered(ctx, email, turn, hex)` | Records that one of the faction's entities stood in `hex`. |
| `VisibleHexes(ctx, email)` | The coordinates the faction knows on the turn the game is on. |

`KnowledgeAsOf` is one read for a faction and a turn rather than a lookup per
hex. That is what its callers need: the orders page prices every one of an
entity's orders on every write, for every player editing orders, so a per-hex
query in a loop is the wrong shape.

`MarkEntered` is monotone and idempotent. Calling it twice for one hex in one
turn leaves what calling it once would have left, and turn processing may walk a
faction's entities in any order.

Both refuse a turn the game cannot be on, and both refuse an account that
controls no faction with `ErrUnknownFaction`. `VisibleHexes` answers such an
account with nothing rather than an error: it is a floor, not a rendered state.

## Determinism

Knowledge introduces no randomness, so it needs no roller and no domain tag. It
is derived from the stored orders, the seeds, and facts dated at or before the
turn, so a replay reproduces it exactly. Because it is frozen at the turn
boundary, no entity's march reveals ground that prices another entity's march in
the same turn. See `internal/prng/doc.go`.
