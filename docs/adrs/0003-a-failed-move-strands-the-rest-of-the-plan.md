# ADR 0003: A failed move strands the rest of the plan

- Status: Proposed
- Date: 2026-09-12

## Context

A turn's orders are executed one at a time against where the entity actually
stands. `docs/reference/action-points.md` states the current rule plainly:

> Steps are resolved in order against where the entity actually stands. If step
> 2 fails, step 3 is priced and walked from the hex the entity did not leave,
> not from the hex the plan assumed.

That rule treats each order as independent. It is not: a move's meaning depends
entirely on where the entity is standing when the move is read. A player who
writes `Move E, Move SW` is not asking for two unrelated things. They are
describing one route.

### What the playtest found

A leader stood at `(186, 46)`. The faction's map already showed ocean at
`(187, 45)` and `(187, 46)`. Turn 7 was ordered `Move E, Move SW`, and the
compass vectors are `E = (+1, 0)` and `SW = (-1, +1)`:

| | The player's route | What the executor walks |
| --- | --- | --- |
| start | `(186, 46)` | `(186, 46)` |
| Move E | `(187, 46)` | fails on terrain, stays at `(186, 46)` |
| Move SW | `(186, 47)` | `(185, 47)` |

Both `(186, 47)` and `(185, 47)` are grassland, so the second step does not
fail. It succeeds, and it is charged 3 AP as unexplored ground. The leader ends
one hex west of where the plan put it, on a route nobody ordered, and nothing
in the turn report says anything is wrong with the second row — it reads
`Carried out`.

That is the defect. A blocked first step does not merely lose its own hex; it
re-anchors every step after it, and the entity walks a plan the player did not
write. The failure is loud. The drift is silent.

The run that produced this is described in the playtest notes for issue #71.
The turn as actually ordered was `Move E, Move E`, where both steps failed the
same way and the flaw is invisible; the `Move SW` variant above is the same
position with the second order changed, and it is the case that matters.

### The asymmetry with exhaustion

The executor already terminates a plan for the other failure kind. From
`internal/game/executor.go`:

> Every order from the first the entity cannot afford is exhausted: it is not
> carried out, it charges nothing, and the entity stops where it stopped.

So a plan that runs out of points stops dead, and a plan that walks into water
carries on from the wrong hex. Both are "the entity is not where the next order
assumed", and only one of them is handled. The asymmetry is not a decision
anyone recorded; it is what falls out of pricing exhaustion globally and
terrain per step.

### Why `blocked` is not the word for it

`docs/reference/turn-results.md` reserves `blocked` for "Something in the
destination prevented entry" — a stacking rule, or another faction's entity —
and notes that nothing produces it yet. That is a property of the destination
hex. An order that was never attempted because an earlier one failed is a
property of the *plan*, so reusing `blocked` would collapse two different
questions into one word and spend a reservation the rules will want later.

## Decision

**When a move is not carried out, every order after it in the same turn is
stranded.** A stranded order is not executed, charges nothing, reveals nothing,
and leaves the entity where it stands.

This adds a fifth value to the failure vocabulary:

| Reason | Meaning |
| --- | --- |
| `stranded` | An earlier order in this turn did not happen, so the entity was not where this order assumed. |

The entity's ledger is unchanged in shape: the failed move is charged what it
cost, everything after it charges nothing, and the unspent allowance lapses.
Bob's turn 7 becomes `spent 1, lapsed 5` rather than `spent 2, lapsed 4`.

Three things follow directly and are part of this decision:

- **It applies to the whole order list, not only to the moves in it.** A rest
  after a failed move is stranded too. The argument is not that rest depends on
  position — it does not — but that the alternative requires every order kind
  the game ever gains to declare whether it is anchored to a place, and that
  classification has to stay correct forever. Stopping the turn is one rule;
  sorting orders into position-dependent and position-free is a rule plus a
  growing table.
- **It applies to every failure kind, including `unknown`.** A move with no
  direction is an order the player meant to move them somewhere. Treating it as
  a no-op that the rest of the plan walks through preserves exactly the defect
  this ADR is about.
- **The pre-processor does not gain a verdict.** The estimate keeps warning and
  keeps pricing the tail as though every order lands. See the open question
  below; changing that is a separate decision, and this one does not depend on
  it.

## Open questions

These are game and presentation rules that this ADR does not settle. They are
the reason its status is Proposed.

1. **The unwarnable case.** The pre-processor is forbidden from warning about
   ground the faction has not seen, because warning there would disclose what
   is under the fog. Under this rule, a leader exploring into unseen water
   loses its entire remaining turn, with no warning possible and no way for the
   player to have known. That is the strongest argument against, and it is a
   fairness judgement rather than a technical one. Three mitigations are
   available and none is chosen here:
   - accept it, on the grounds that exploration is priced as risk already;
   - strand the remainder but treat the unspent points as rested, so the turn
     is lost but the allowance is not wasted;
   - strand only when the failure was *foreseeable* — when the faction already
     knew the terrain — which makes the executor's behaviour depend on the
     player's knowledge. That is defensible and is a larger change than it
     looks.
2. **What the estimate shows.** `action-points.md` argues that the
   pre-processor must not refuse a step, because a refusal that turns out wrong
   mis-prices every order after it and costs the player the whole turn rather
   than one row. This rule inverts half of that argument: if a warned step will
   in fact strand the tail, then pricing the tail from the assumed position is
   now the misleading answer. But the pre-processor can still be wrong, so a
   confident cascade in the estimate would be a new way to lie. A conditional
   projection — "4 AP, or 1 AP if the warned step does not land" — is a third
   answer with its own cost.
3. **The word.** `stranded` is proposed. `aborted` says more about the order
   and less about the entity. The vocabulary is a compatibility surface for the
   API and the reports, so the choice is worth making once.
4. **Whether a player can express a fallback.** Today "try east, otherwise go
   south-west" is sometimes expressible by accident, because a failed step
   leaves the next one running from the old hex. This rule removes that, and
   the game gains no conditional order to replace it. That is arguably correct
   — the accident was never a feature — but it should be a deliberate gap
   rather than an unnoticed one.

## Consequences

- `docs/reference/action-points.md` loses its "A failed step does not move the
  entity" rule and gains this one. The section is currently explicit that step
  3 is walked from where the entity stands; under this ADR step 3 is not walked
  at all.
- `docs/reference/turn-results.md` gains `stranded` in the reason table, in the
  observation table (a stranded order reveals nothing, where a terrain failure
  still observes the hex it walked into), and in the sentence list the report
  renders.
- `internal/game/executor.go` stops the walk on the first uncarried order, the
  way it already does for exhaustion. The change is small and sits entirely in
  `Execute`; `Price` is untouched unless open question 2 is answered against
  the default.
- `internal/server/results.go` needs a sentence for `stranded`. It already
  prints an unknown reason with the word the turn recorded, so the report
  degrades legibly until the sentence is written.
- The API's results resource gains a value in a documented enumeration.
  Existing clients see a reason they have not been told about, which the
  reference already anticipates for `blocked`.
- A single misjudged step now costs a whole turn rather than one row. That is
  the intended effect and also the main risk; see open question 1.
- Failure becomes more visible rather than less. A turn where four orders read
  `stranded` states plainly that the plan stopped at row 1, which is a better
  report than four rows of plausible-looking movement. This interacts with #71,
  where the complaint is that the app never escalates.

## Alternatives considered

### Leave the rule as it is

Each order is resolved against the entity's real position and the player
reconciles the difference from the turn report, which since #59 does state
every failure. This is cheapest and it is not obviously wrong for a player who
reads carefully. It was rejected because the drift it produces is silent: the
re-anchored step reports `Carried out`, so the report that was built to close
this loop says nothing about the row where the plan actually went wrong.

### Strand only the moves, and let other orders run

A rest does not depend on where the entity stands, so a plan of `Move, Move,
Rest 2` could still rest. This is kinder and it is the reading a player would
probably guess. It was rejected as the default because it requires a permanent
per-order-kind classification, and because the kinder outcome it buys is better
obtained through open question 1's second mitigation — treating the stranded
remainder as rested — which needs no classification at all.

### Re-plan the tail against the new position

Walk the remaining orders as directions relative to wherever the entity ended
up, which is what happens today, but say so in the report: mark every order
after a failure as re-anchored. This keeps the entity moving and makes the
drift visible rather than silent. It was rejected because a route re-anchored
one hex off is not a worse version of the player's plan, it is a different
plan, and reporting that clearly does not make walking it correct.

### Make the pre-processor refuse the step

If the estimate simply declined to price a step into known-impassable ground,
the player would fix the order before the turn ran. This is rejected by
`action-points.md` on grounds this ADR does not disturb: the pre-processor
models the rules it knows and no more, and a refusal that turns out wrong is a
lie that costs the player everything after it.
