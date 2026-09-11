# Action points reference

What an entity may do in a turn, what each thing costs it, and what happens when
it runs out.

The costs, the allowance and the order pre-processor are implemented by
`internal/game` (`actionpoints.go`), `internal/datastore` (`preprocessor.go`)
and `internal/server` (`orders.go`). Turn processing, which is what charges
them, is `executor.go` in the first two of those; see
[Turn processing reference](turn-processing.md). What it charged is recorded
per entity per turn; see [Turn results reference](turn-results.md#the-ledger).

## Vocabulary

| Term | Definition |
| --- | --- |
| Action point | The unit an entity spends to act. Abbreviated AP. |
| Allowance | How many action points an entity has for one turn. |
| Rest | An order kind that spends action points and moves nothing. |
| Exhaust | The failure of a step an entity cannot afford. |
| Known | Of a hex: the faction had observed or explored it when the turn opened. |
| Pre-processor | What prices an entity's orders during order entry and reports the points they leave idle. Binds nothing. |
| Executor | What walks an entity's orders when the turn is processed and charges them. Decides what happened. |
| Estimate | What the pre-processor answers with. Every order is priced as though it lands, and unknown ground at a flat cost. |

An [order](orders.md) is one action, so a `move` is one step and a move's cost
is a step's cost. *Step* is the grain a turn's results are recorded on.

Which hexes a faction knows is its observed and explored record; see
[Knowledge reference](knowledge.md). This document uses *known* for either
state, because a step costs the same onto ground the faction has walked as onto
ground it has only seen.

## The allowance

An entity has an allowance of action points for a turn. A newly created leader's
is `6`.

The allowance is a [fact](entities.md#effective-dating) of the entity,
effective-dated like its location, and it is not a running balance. Nothing
carries into the next turn: a turn opens at the allowance effective on that
turn, that is the whole of what the entity may spend, and turn processing never
writes a total back. It is stored in `entity_allowances` and written at creation
from `game.FoundingAllowance`; see [Datastore](../DATASTORE.md#fact-tables).

An entity whose kind accepts no orders has no allowance. A hamlet and Marajanda
accept nothing, so they have none, and having none is the absence of a row
rather than a zero in one.

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

Ocean, lake, and ice stop a leader or hamlet. Marajanda is not stopped by
terrain and may enter all three. A coordinate outside the world has no terrain
to ignore and stops every kind. The check is on the destination only, so an
ordinary entity already standing in water may leave it by stepping onto land.
See [Terrain reference](terrain.md).

### The estimate warns, the executor decides

The first row of that table is a case the pre-processor can see coming: the
faction knows the hex, so the page can say so before the turn is processed
rather than leaving a player to work it out from where their leader did not end
up. Each order in an estimate therefore carries a warning, empty when there is
nothing to say, in the same vocabulary the result records as its reason.

The two are deliberately different powers. **The pre-processor warns and the
executor decides.** A warning does not change what the order costs, does not
move the entity, and does not stop the walk: the row is still priced, and the
orders after it are still priced from the destination, because every order is
assumed to land.

That is not timidity. The pre-processor models the rules it knows and no more,
so a step it calls impossible may still happen — an effect nobody taught it
about is the kind of thing a game acquires. A warning that turns out wrong is a
warning. A refusal that turns out wrong is a lie, and every order after it would
be priced from a hex the entity is not standing in, so one wrong call would cost
the player their whole turn rather than one row of it.

It warns about two things and no others:

- a coordinate the world does not have, since rows do not wrap and the world's
  extent is published rather than hidden, and
- impassable ground the faction already knows, which is already drawn on their
  map in the colour of the water they are about to walk into.

It is silent about ground the faction has not seen. Warning there would say what
is under the fog, which is the whole reason an unknown hex is priced flat; the
exploration price carries that risk instead, as a real cost rather than as a
warning.

## A failed step does not move the entity

Steps are resolved in order against where the entity actually stands. If step 2
fails, step 3 is priced and walked from the hex the entity did not leave, not
from the hex the plan assumed. A plan is exactly priceable only while every step
lands.

## Exhaust

An entity is walked until it cannot afford its next step. The steps it took
stand, it stops where it stopped, and each remaining step is recorded as failed
for `exhaust`. An order after the crossing exhausts whether or not it would have
cost less than the one that crossed. See
[Turn processing reference](turn-processing.md#outcomes).

Nothing is rejected at order entry for being too long. `MaxOrdersPerEntity`
bounds how many orders an entity may carry in a turn; it is a storage bound and
not an allowance. See [Orders reference](orders.md#storage).

## Rest

| Kind | Accepts | Steps | Cost |
| --- | --- | --- | ---: |
| `rest` | `leader` | none | 1 AP each |

A rest carries a count and costs one action point per point of it, so a
`Rest x3` is one order with one cost rather than three rows. The count is at
least one: a `Rest x0` is an order that costs nothing and does nothing, and
nothing stores one.

A rest may be ordered more than once in a turn, and it may sit anywhere in an
entity's list. Ordering one before a move is legal: it spends the AP where it
was asked for and leaves the move to exhaust. A rest at the *end* of a list is
an order like any other; see [Idle action points](#idle-action-points).

What a rest recovers is open. Nothing tracks a condition a rest could restore,
so a rest today costs its AP, records that it happened, and changes no state.
[#36](https://github.com/mdhender/marajanda/issues/36) decides what it gives.
Its cost and its position among the order kinds do not change when that answer
arrives.

## The two engines

Two things price orders, and the distinction is the point.

| | Serves | Writes orders | Binding |
| --- | --- | --- | --- |
| Pre-processor | Order entry. Prices the set and reports the idle points. | No | **No** |
| Executor | Turn processing. Walks the orders and charges them. | No | Yes. It decides what happened. |

`game.Price` is the walk and `game.Execute` reads it as a result: what an entity
is charged is what the estimate committed, so the two cannot drift.

The invariant is the narrow one: **the executor writes no orders.** Everything
the pre-processor does is the player acting through the page.

Both price through one function, `game.Price`, so the two agree exactly over
ground the faction knows. Where they differ, the difference is exploration and
nothing else.

### The pre-processor's numbers are an estimate

The word is not hedging, it names two specific approximations.

- **Every order is assumed to land.** Row `n` is priced from where rows `1..n-1`
  would leave the entity. A step that fails leaves the entity elsewhere, and
  every row after it was priced from a hex it never reached.
- **Unknown ground is priced flat.** An unknown hex costs the exploration price
  whatever is actually there. A cost that varied with terrain the faction has
  not seen would tell a player what is there, so it does not vary. The executor
  charges the real cost, the difference comes out of the entity's remaining
  orders, and the tail of the turn exhausts. That is the risk of exploration,
  priced as a real cost rather than as a warning.

An order that cannot be priced at all — a move a player has added and not yet
said the direction of — is shown unpriced rather than as costing nothing.

### Accuracy is a parameter, never a request

What a costing may see is `game.Sight`. Its zero value is fogged, and a
ground-truth costing cannot be built without handing over a world to read.

| Sight | Reads | Used by |
| --- | --- | --- |
| Fogged | The faction's knowledge. Every order lands. | The orders page, for a player |
| Ground truth | The knowledge *and* the world. A step that will fail is shown as failing, and the orders after it are priced from the hex the entity did not leave. | The executor, and the admin path |

The level is set from the session's role and never from anything in a request.
A player who hand-builds a request cannot ask for the accurate answer, because
there is nothing in a request that says which answer to give.

The pairing is what the two engines are tested against: over ground the faction
knows in full, the fogged estimate and the ground truth agree on every row.

### Whole-set recalculation

Every write re-prices every row of the entity's list. Inserting or removing an
order changes where the entity stands for every order after it — a move that was
onto known ground may now be onto unknown ground, or the reverse — so any scheme
that re-priced only the touched row would be wrong.

It needs no endpoint of its own. Every write already returns the whole
re-rendered `#orders` region, so the pre-processor runs during that render and
the costs ride along in markup already being sent. See
[Orders reference](orders.md#page).

## Idle action points

Unspent action points are the player's business, and they stay the player's
business: nothing spends them on an entity's behalf. What an entity's orders
leave over are its **idle** points, and the pre-processor reports the number as
`Estimate.Residue`.

They are a number and never an order. An entity may be spent to exhaustion
deliberately, and a rest written into the list by anything other than the player
would take that choice away — quietly, and in a row the player never typed. The
same rule is why the executor writes no orders; this is the pre-processor half
of it.

**The line is rendered always, whatever the count.** The page therefore keeps
its shape as a player edits, which is the convention the faction picker follows.
Zero idle points is a true statement about a number, so the line reads `0 idle`
rather than naming an order that does not exist.

A rest at the end of a list is an order like any other: it costs what it says,
it is drawn with its own controls, and it is not resized. What the list leaves
over after it is idle. Nothing has to tell a player's rest apart from anything
else, because there is nothing else.

Storing the residue was the older design, and it cost more than it paid for.
The row had to come off before every write and go back after it, so a write to
one order rewrote another; an append landed exactly where the residue sat and
was replaced by the write that followed it
([#56](https://github.com/mdhender/marajanda/issues/56)); and the count was
sized from an estimate, which is the one thing a durable row must not be sized
from. A number computed when asked has none of those problems.

A plain read stores nothing. Opening the orders page and leaving it prices the
turn and writes no rows; the idle count is still drawn.

That count is exact only while every step lands. A move that fails partway
leaves the entity somewhere else, and the true residue may differ from the
projection.

Turn processing appends nothing either. Action points still unspent when an
entity's orders run out lapse, and the turn result records how much lapsed.
Lapsing is the normal outcome for points nobody ordered spent — a player who
wants them spent resting says so with a Rest order.

### Resting the idle points

Saying so by hand, every turn, and counting the points correctly is work the
page can do. Each entity's budget line carries a control that writes the rest:

> **Rest the remaining 4 points**

It is an action and not a mode. One press appends one `Rest`, and nothing is
maintained afterwards — no preference is stored, no later write resizes it, and
a player who changes their plan presses it again. Naming the number means the
control also answers "how many have I got left" on the way past.

What it writes is an ordinary authored order. It is stored like one, drawn with
its own count box and remove control, and indistinguishable from one a player
typed, because it is one. A list that already ends in a rest gets a second one
rather than having the first grown: the button never edits an order the player
wrote, which is the ambiguity [#56](https://github.com/mdhender/marajanda/issues/56)
was about.

The number on the button is a label. The button posts the entity and never the
count, and the residue is read again when the write lands, so a page held open
while the orders moved cannot write a count nobody's plan justifies.

Nothing to rest is a disabled control with the reason beside it — zero idle
points reads "These orders spend every point", and a plan already past its
allowance reads how far past. The control is drawn disabled rather than left
out, so the line keeps its shape ([#58](https://github.com/mdhender/marajanda/issues/58)),
and the attribute is the real `disabled` rather than a styling of one. The page
saying so is a courtesy: the store refuses `Rest x0` on its own account, so a
request built by hand is refused too.

This needs nothing of the API, which is already expressive enough: a client
reads `residue` from `GET /api/v1/orders` and appends a rest of that length
through `POST /api/v1/entities/{entity}/orders`, which is what the control does
on the player's behalf. See [#64](https://github.com/mdhender/marajanda/issues/64).

## Overspend

Overspending is allowed during entry as a courtesy and bounded by
`MaxOrdersPerEntity`. It is not an obligation, and it is not tolerated when the
turn is processed: the entity is walked until it cannot afford its next order,
the orders it could afford stand, and each one it could not is recorded as
exhausted. The excess is rejected, not the set.

The page owes the player the running total, the row where the committed cost
crosses the allowance, and a mark on every row at or after it that will exhaust.
There are no negative idle points to render: the residue floors at zero, and
what the orders cost beyond the allowance is reported as the overspend instead.

## Founding

A faction's homeland ring is known in full when the faction is seated: the
origin hex explored, its six neighbours observed. Those facts are effective from
the founding turn, the exception [Entities reference](entities.md#effective-dating)
already carves out, because nothing about founding waits on a turn to be
processed. See [Knowledge reference](knowledge.md#what-writes-it).

Turn 1 therefore opens with seven known hexes: a leader may spend all six points
walking the ring cheaply, or step out and explore.

## What changes an allowance

Nothing does. `6` is written when a leader is created and stays. No order, no
terrain, and no event raises or lowers an allowance, and no rule yet gives one
to an entity kind other than `leader`.
