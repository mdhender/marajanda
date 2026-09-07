# Action points reference

What an entity may do in a turn, what each thing costs it, and what happens when
it runs out.

Nothing implements these rules yet. `rest_orders` is defined ahead of the kind
that writes to it; see [Orders reference](orders.md#storage). The decisions this
document records are in
[#28](https://github.com/mdhender/marajanda/issues/28) and
[#33](https://github.com/mdhender/marajanda/issues/33).

## Vocabulary

| Term | Definition |
| --- | --- |
| Action point | The unit an entity spends to act. Abbreviated AP. |
| Allowance | How many action points an entity has for one turn. |
| Rest | An order kind that spends action points and moves nothing. |
| Exhaust | The failure of a step an entity cannot afford. |
| Known | Of a hex: the faction had observed or explored it when the turn opened. |

An [order](orders.md) is one action, so a `move` is one step and a move's cost
is a step's cost. *Step* is the grain a turn's results are recorded on.

Which hexes a faction knows is its observed and explored record, which
[#38](https://github.com/mdhender/marajanda/issues/38) owns. This document uses
*known* for either state, because a step costs the same onto ground the faction
has walked as onto ground it has only seen.

## The allowance

An entity has an allowance of action points for a turn. A newly created leader's
is `6`.

The allowance is a [fact](entities.md#effective-dating) of the entity,
effective-dated like its location, and it is not a running balance. Nothing
carries into the next turn: a turn opens at the allowance effective on that
turn, that is the whole of what the entity may spend, and turn processing never
writes a total back.

An entity whose kind accepts no orders has no allowance. A hamlet accepts
nothing, so it has none.

## What a step costs

| Destination hex | Cost |
| --- | ---: |
| The faction knew it when the turn opened | 1 AP |
| It did not | 3 AP |

The cost is flat regardless of terrain: a step into mountains costs what a step
into grassland costs. The 3 AP is the price of walking into ground the faction
cannot describe.

The flat pair is a deliberate approximation, standing in for a cost that is a
function of the moving entity's race, the terrain it leaves and the terrain it
enters. [#37](https://github.com/mdhender/marajanda/issues/37) replaces it.
Only the numbers change, not the shape of the rule.

## Knowledge is frozen at the turn boundary

A step is priced against what the faction knew when the turn opened. Every
reveal a step produces is effective from `turn + 1`, like every other
consequence of a turn, so nothing learned during turn N prices a step in turn N.

Three consequences follow.

- **The second ring always costs 3.** An entity knows the six neighbours of the
  hex it stands in, so its first step costs 1 AP. The ring beyond that was not
  known when the turn opened, so the next step costs 3 AP whatever the first
  step revealed. Six points buy one cheap step and one exploration with 2 AP
  left: enough to walk back onto known ground, not enough to explore again.
- **Retracing this turn's own new ground costs 3 again.** An entity that
  explores a hex and steps back into it pays 3 twice.
- **Processing is order-independent for cost.** No entity's march makes another
  entity's march cheaper, so a turn's result does not depend on which entity is
  walked first.

Because every step's cost is fixed before the turn is processed, a plan can be
priced exactly at order entry.

## Impassable destinations

| Case | Cost | Outcome |
| --- | ---: | --- |
| The faction knew the destination is impassable | 1 AP | No movement. Fails for `terrain`. |
| The destination was unknown and turns out impassable | 3 AP | No movement, and the hex becomes known. Fails for `terrain`. |

A step is paid for as what it was when it was ordered. Walking into ice the
faction had been told about costs the ordinary step price, because there was
nothing to learn and nothing was learned. Walking into ice it had not seen costs
the exploration price, because the exploration happened: the faction now knows
that hex, and the entity is standing where it started.

Terrain marks ocean and lake passable and no rule yet says an entity needs a
boat. Whether water may be entered is open, and belongs to
[#37](https://github.com/mdhender/marajanda/issues/37). See
[Terrain reference](terrain.md).

## A failed step does not move the entity

Steps are resolved in order against where the entity actually stands. If step 2
fails, step 3 is priced and walked from the hex the entity did not leave, not
from the hex the plan assumed. A plan is exactly priceable only while every step
lands.

## Exhaust

An entity is walked until it cannot afford its next step. The steps it took
stand, it stops where it stopped, and each remaining step is recorded as failed
for `exhaust`.

Nothing is rejected at order entry for being too long. `MaxOrdersPerEntity`
bounds how many orders an entity may carry in a turn; it is a storage bound and
not an allowance. See [Orders reference](orders.md#storage).

## Rest

| Kind | Accepts | Steps | Cost |
| --- | --- | --- | ---: |
| `rest` | `leader` | none | 1 AP each |

A rest may be ordered more than once in a turn, and it may sit anywhere in an
entity's list. Ordering one before a move is legal: it spends the AP where it
was asked for and leaves the move to exhaust.

What a rest recovers is open. Nothing tracks a condition a rest could restore,
so a rest today costs its AP, records that it happened, and changes no state.
[#36](https://github.com/mdhender/marajanda/issues/36) decides what it gives.
Its cost and its position among the order kinds do not change when that answer
arrives.

## The engine never writes orders

Unspent action points are the player's business. The orders page carries a
trailing `Rest xN` stanza that starts at the entity's whole allowance and
decrements as move steps are added, so a player sees where their six points went
and the stored orders are exactly what the player agreed to.

That count is exact only while every step lands. A move that fails partway
leaves the entity somewhere else, and the true residue may differ from the
projection.

Turn processing appends nothing. Action points still unspent when an entity's
orders run out lapse, and the turn result records how much lapsed. That is a
reporting line, not a rule.

## Founding

A faction's homeland ring is known in full when the faction is seated: the
origin hex explored, its six neighbours observed. Those facts are effective from
the founding turn, the exception [Entities reference](entities.md#effective-dating)
already carves out, because nothing about founding waits on a turn to be
processed.

Turn 1 therefore opens with seven known hexes: a leader may spend all six points
walking the ring cheaply, or step out and explore.

## What changes an allowance

Nothing does. `6` is written when a leader is created and stays. No order, no
terrain, and no event raises or lowers an allowance, and no rule yet gives one
to an entity kind other than `leader`.
