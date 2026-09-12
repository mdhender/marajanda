# ADR 0003: A failed move strands the rest of the plan

- Status: Proposed
- Date: 2026-09-12
- See also: ADR 0004, which restates this rule as a per-order guard

## Context

A turn's orders are executed one at a time against where the entity actually
stands. `docs/reference/action-points.md` states the current rule plainly:

> Steps are resolved in order against where the entity actually stands. If step
> 2 fails, step 3 is priced and walked from the hex the entity did not leave,
> not from the hex the plan assumed.

That rule treats each order as independent. It is not: a move's meaning depends
entirely on where the entity is standing when the move is read. A player who
writes `Move E from (186, 46) to (187, 46)` and then `Move SW from (187, 46) to
(186, 47)` is not asking for two unrelated things. They are describing one
route, and the second row says so in as many words.

### What the playtest found

A leader stood at `(186, 46)`. The faction's map already showed ocean at
`(187, 45)` and `(187, 46)`. Turn 7 was ordered two moves, written here in the
full form proposed by #72, with compass vectors `E = (+1, 0)` and
`SW = (-1, +1)`:

```
1  Move E  from (186, 46) to (187, 46)
2  Move SW from (187, 46) to (186, 47)
```

Row 1 fails on terrain and the leader does not leave `(186, 46)`. Row 2 says it
starts at `(187, 46)`, and the entity is not there. The executor runs it
anyway, against wherever the entity actually stands:

| | What the orders say | What the executor walks |
| --- | --- | --- |
| start | `(186, 46)` | `(186, 46)` |
| `Move E from (186, 46) to (187, 46)` | `(187, 46)` | fails on terrain, stays at `(186, 46)` |
| `Move SW from (187, 46) to (186, 47)` | `(186, 47)` | `(185, 47)` |

Both `(186, 47)` and `(185, 47)` are grassland, so the second step does not
fail. It succeeds, and it is charged 3 AP as unexplored ground. The leader ends
one hex west of where the plan put it, on a route nobody ordered, and nothing
in the turn report says anything is wrong with the second row — it reads
`Carried out`.

That is the defect. A blocked first step does not merely lose its own hex; it
re-anchors every step after it, and the entity walks a plan the player did not
write. The failure is loud. The drift is silent.

Written out in full it is almost self-evident: row 2 states the hex it starts
from, that statement is false by the time it runs, and the executor has no rule
that cares. Written as `Move SW` there is nothing to notice, which is why #72
and this ADR are the same observation approached from two sides.

The run that produced this is described in the playtest notes for issue #71.
The turn as actually ordered was `Move E from (186, 46) to (187, 46)` twice
over, where both steps failed the same way and the flaw is invisible; the
`Move SW` variant above is the same position with the second order changed, and
it is the case that matters.

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

The turn above becomes:

```
1  Move E  from (186, 46) to (187, 46)   1 AP   Did not happen - it could not enter that hex
2  Move SW from (187, 46) to (186, 47)   —      Stranded - the entity never reached (187, 46)
```

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
  keeps pricing the tail as though every order lands. See open question 1
  below; changing that is a separate decision, and this one does not depend on
  it.

### The question this rule does not answer

The rule is stated above without qualification, and there is one case where
that is hard to defend. It belongs here rather than in a list at the end,
because it is an objection to the decision itself and not a detail of carrying
it out.

The pre-processor is forbidden from warning about ground the faction has not
seen — `action-points.md` is explicit that warning there would disclose what is
under the fog, and that the flat exploration price is what covers the risk
instead. So consider the same two orders written against unexplored ground:

```
1  Move E  from (186, 46) to (187, 46)   3 AP   ← unknown ground, no warning is permitted
2  Move SW from (187, 46) to (186, 47)   3 AP
```

If `(187, 46)` turns out to be water, the player loses their entire remaining
turn to a step the game was not allowed to warn them about and that they had no
way to know was impossible. Under today's rule they lose one row. Under this
one they lose the turn.

That is the strongest argument against this ADR, and it is a fairness
judgement rather than a technical one, so it is not settled here. Three answers
are available:

- **Accept it.** Exploration is priced as risk already, and a plan written
  blind is a gamble the player chose to make.
- **Strand the remainder, but rest it.** The turn is lost and the allowance is
  not wasted. This needs no new classification of order kinds and is the
  cheapest mitigation.
- **Strand only what was foreseeable.** Cascade when the faction already knew
  the terrain, and fall back to today's behaviour when it did not. This makes
  the executor's consequences depend on the player's knowledge, which is
  defensible — the player is only held to a plan they could have checked — and
  is a larger change than it looks, because the executor would then need the
  faction's knowledge as well as the ground truth.

The first two keep one rule. The third keeps the game kinder and buys it with a
rule that has an exception in it.

## Open questions

These are game and presentation rules that this ADR does not settle, beside the
fairness question raised with the decision above. Together they are the reason
its status is Proposed.

1. **What the estimate shows.** `action-points.md` argues that the
   pre-processor must not refuse a step, because a refusal that turns out wrong
   mis-prices every order after it and costs the player the whole turn rather
   than one row. This rule inverts half of that argument: if a warned step will
   in fact strand the tail, then pricing the tail from the assumed position is
   now the misleading answer. But the pre-processor can still be wrong, so a
   confident cascade in the estimate would be a new way to lie. A conditional
   projection — "4 AP, or 1 AP if the warned step does not land" — is a third
   answer with its own cost.
2. **The word.** `stranded` is proposed. `aborted` says more about the order
   and less about the entity. The vocabulary is a compatibility surface for the
   API and the reports, so the choice is worth making once.
3. **Whether a player can express a fallback.** ~~Today "try east, otherwise go
   south-west" is sometimes expressible by accident, because a failed step
   leaves the next one running from the old hex. This rule removes that, and
   the game gains no conditional order to replace it.~~

   **Answered, and it changes this ADR.** If #72's hexes are authored, a
   fallback is already expressible: an order that names the hex the failed
   order *started* from, rather than the one it aimed at, is an instruction to
   do that instead. ADR 0004 restates the cascade below as a per-order guard —
   *an order that says where it starts runs only if the entity is standing
   there* — which produces this ADR's behaviour for moves and gets fallbacks
   for nothing.

   It does not agree with this ADR everywhere. Under 0004 an order that states
   no origin is unguarded and runs, so the first bullet below — that the rule
   applies to the whole order list, a rest included — is the one thing 0004
   decides differently rather than merely re-expresses. If 0004 is accepted,
   the Decision below is subsumed by it and this ADR should be marked
   superseded. The defect described in the Context, and the fairness question
   raised with the decision, stay live either way.

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
  `Execute`; `Price` is untouched unless open question 1 is answered against
  the default.
- `internal/server/results.go` needs a sentence for `stranded`. It already
  prints an unknown reason with the word the turn recorded, so the report
  degrades legibly until the sentence is written.
- The API's results resource gains a value in a documented enumeration.
  Existing clients see a reason they have not been told about, which the
  reference already anticipates for `blocked`.
- A single misjudged step now costs a whole turn rather than one row. That is
  the intended effect and also the main risk; see the fairness question raised
  with the decision.
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

A rest does not depend on where the entity stands, so a plan that moves twice
and then rests could still carry out its `Rest 2` after the first move failed.
This is kinder and it is the reading a player would probably guess. It was
rejected as the default because it requires a permanent
per-order-kind classification, and because the kinder outcome it buys is better
obtained through the second mitigation offered with the decision — treating the
stranded remainder as rested — which needs no classification at all.

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
