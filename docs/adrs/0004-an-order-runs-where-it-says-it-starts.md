# ADR 0004: An order runs where it says it starts

- Status: Proposed
- Date: 2026-09-12

## Context

Two proposals are open that look unrelated:

- **#72** would write a move as `Move E from (186, 46) to (187, 46)` rather than
  as `Move E`, and asks whether those hexes are authored by the player or
  derived by the server.
- **ADR 0003** would make a failed move strand every order after it in the
  turn, so that a plan does not carry on from a hex the entity never reached.

If the hexes are authored, these are the same rule. ADR 0003 left an open
question asking whether a player could express a fallback — "try east,
otherwise go south-west" — and concluded that the game had no way to say it.
That was wrong. Authored hexes already say it:

```
1  Move E  from (186, 46) to (187, 46)
2  Move SE from (186, 46) to (186, 47)
```

Row 2 starts from the hex row 1 starts from, not the hex row 1 ends at. It is
not a continuation of row 1. It is what to do instead of row 1, and it says so
without a single word of new syntax.

ADR 0003 reached for a cascade — *everything after a failure is stranded* —
because it was reasoning about positions in a list. The list is not the thing
that matters. What matters is whether the entity is standing where the order
was written for.

## Decision

**An order that says where it starts is carried out only if the entity is
standing there.** An order whose stated origin is not where the entity stands
is stranded: not executed, charging nothing, revealing nothing. An order that
states no origin is not guarded and runs wherever the entity has ended up.

The origin is the guard. Whether an order has one is a property of its kind,
settled below.

This replaces ADR 0003's cascade with the rule it was approximating. A move
always states an origin, so for moves it produces ADR 0003's behaviour exactly:

| Row 2 says | Row 1 failed | Row 1 succeeded |
| --- | --- | --- |
| `from (187, 46)` — the hex row 1 aimed at | stranded | runs |
| `from (186, 46)` — the hex row 1 started from | runs | stranded |

The first line is a continuation and behaves exactly as ADR 0003 proposed. The
second line is a fallback, and it costs nothing to support because it is the
same comparison answered the other way.

### It extends to following orders without extending the rule

A plan with a fallback in it continues on both branches by writing both
continuations. Each row's origin says which branch it belongs to:

```
1  Move E  from (186, 46) to (187, 46)     the attempt
2  Move SE from (186, 46) to (186, 47)     instead of row 1, if row 1 failed
3  Move E  from (187, 46) to (188, 46)     after row 1, if row 1 landed
4  Move SE from (186, 47) to (186, 48)     after row 2, if row 2 landed
```

- Row 1 lands: entity at `(187, 46)`. Row 2 is stranded, row 3 runs, row 4 is
  stranded. The turn ends at `(188, 46)`.
- Row 1 fails: entity at `(186, 46)`. Row 2 runs, row 3 is stranded, row 4
  runs. The turn ends at `(186, 48)`.

The order list is a decision tree written out flat. The executor never learns
what a branch is; it asks one question per row and the tree falls out.

### Whether an order has an origin is a property of its kind

Three policies, and an order kind declares which one it follows:

| Policy | Kinds | Effect |
| --- | --- | --- |
| Required | `move` | Always guarded. A move is meaningless without a place to move from. |
| None | `rest` | Never guarded. Resting does not depend on where the entity is standing. |
| Optional | future kinds | Guarded only when the player writes an origin. |

ADR 0003 refused to classify order kinds this way, on the grounds that it meant
maintaining a table forever. That objection is weaker than it looked. Every
order kind already carries per-kind knowledge — its own detail table in the
schema, its own validation, its own row on the orders page — and the page that
generates orders already knows each row's origin, because the pre-processor
walks the plan to price it. A location policy is one more property of a kind
that has properties, not a new category of thing to keep true.

The third policy is the reason to prefer this over guarding everything
uniformly. An optional origin is a **condition the player opts into**. Take a
`Pray` order that is only worth giving inside a shrine:

```
5  Pray at (186, 48)     only if the entity reached the shrine hex
5  Pray                  wherever the entity ended up
```

Both are legitimate instructions and they mean different things. A rule that
guards every order can only express the first; a rule that guards none can only
express the second. Making the origin optional lets the player say which they
meant, in the same notation that already expresses a fallback.

The player's real condition here is *the hex is a shrine*, and what they write
is *the hex is `(186, 48)`*. That works because a shrine they know about is on
their map. It does not extend to "pray if you happen to find one", which is a
condition on terrain rather than on position and is not what this rule offers.

#### What this means for a rest after a failed move

A rest is unguarded, so it runs even when the move before it failed. ADR 0003
asked what should happen to the other orders in a turn and answered "everything
stops". This answers differently, and better: a player who writes

```
1  Move E  from (186, 46) to (187, 46)
2  Rest 2
```

gets their two points rested whether or not the move landed, because that is
plainly what they meant. It also turns ADR 0003's second fairness mitigation —
*strand the remainder, but rest it* — from a rule the game imposes into
something the player writes for themselves.

## Open questions

1. **The guard is positional, not conditional.** It asks where the entity is,
   not which branch it is on. A route that returns to a hex it has already
   stood in will fire any later row guarded on that hex, whichever branch the
   player wrote it for:

   ```
   1  Move E  from A to B
   2  Move W  from B to A     come back
   3  Move SE from A to C     written as the fallback for row 1
   ```

   Row 3 fires whether row 1 failed or row 1 succeeded and row 2 brought the
   entity home. Whether that is a defect or simply what the player wrote is a
   judgement, and the alternative — tracking branch identity rather than
   position — is a substantially larger rule.
2. **A stale guard and a deliberate fallback are spelled identically.** This is
   the serious cost. If a player changes row 1's direction, row 2's origin is
   now wrong, and the server cannot correct it, because "this origin is not
   where the previous row ends" is precisely what a fallback looks like.
   Derived hexes would simply be rewritten; authored ones cannot be. The
   mitigation is annotation rather than correction: the pre-processor already
   walks the plan and can label every row *runs*, *runs only if an earlier row
   fails*, or *can never run*. A stale guard shows up as the third. Whether
   that is enough protection for a player editing a long list is open.
3. **What the estimate projects.** A fogged costing assumes every order lands,
   which under this rule means the optimistic branch runs and every fallback is
   stranded and unpriced. That is a coherent default and it means a player
   never sees what their fallback would cost. A second projection, or a
   conditional price per row, are the alternatives. This is ADR 0003's open
   question 1 in a sharper form.
4. **How a player authors a fallback.** Authoring an ordinary origin is not a
   problem: the orders page walks the plan to price it, so it already knows
   each row's origin and can fill it in without the player typing a
   coordinate. The open part is the fallback, where the origin the page would
   fill in is the wrong one by definition. The page has to offer something like
   "add a fallback for this row", which means the UI has a concept of branch
   even though the rule does not.
5. **Whether this is too clever.** The rule is simple and what it enables is
   not. A play-by-mail game whose order list is a decision tree asks more of a
   player than one whose order list is a list. The counter-argument is that a
   player who does not want a fallback never writes one and never sees one.

## Consequences

- **ADR 0003's decision becomes a consequence of this one** rather than a rule
  of its own. Its context, its account of the defect, and its fairness question
  all stay live; its cascade is subsumed. If this ADR is accepted, 0003 should
  be marked superseded by it rather than withdrawn, because the reasoning that
  got there is the reasoning for this.
- **It does not agree with ADR 0003 everywhere.** 0003 decided that a failed
  move strands the whole order list, a rest included. Under an origin-as-guard
  rule an unguarded rest runs, so the two differ for every order that states no
  origin. That is a behavioural difference and not a restatement, and it is the
  one place where accepting this ADR changes what 0003 would have done rather
  than merely how it is expressed.
- **#72 is decided by this, not merely informed by it.** A derived origin is by
  definition whatever the previous row left, so row 2 could never say
  `from (186, 46)` while row 1 ends at `(187, 46)`. If fallbacks are wanted,
  the hexes must be authored. That question is no longer a cost comparison.
- **It partly answers ADR 0003's fairness objection.** A player exploring into
  ground the pre-processor may not warn about can now write their own
  protection: a fallback on the step they are unsure of. It does not remove the
  objection — writing a fallback for every exploratory step is tedious, and the
  player still cannot know which step needs one — but "the game gave me no way
  to hedge" stops being true.
- `stranded` from ADR 0003 is still the right reason word, and its definition
  generalizes: *the entity was not where this order said it starts*. It is
  never recorded against an order that states no origin, because such an order
  has nothing to be stranded by.
- **Every order kind now declares a location policy** — required, none, or
  optional — alongside the detail table and validation it already declares. A
  new kind that forgets to choose one is a kind whose guarding behaviour is
  undefined, so the policy belongs where the kind is defined rather than in a
  lookup beside it.
- The comparison must be made against normalized coordinates. `Execute` already
  normalizes the entity's position through `plan.World.Normalize`; an authored
  origin has to go through the same door, or the world's wrap will make two
  spellings of one hex fail to match.
- Stranded orders charge nothing, so contingency branches are free to write and
  only the path taken is paid for. `MaxOrdersPerEntity` is 32 against a 6 AP
  allowance, so the bound on a tree is how much of it a player will type, not
  what it costs them.
- Order editing gets harder in a way the current API does not anticipate.
  `POST`, `PATCH` and `DELETE` all address one row, and under authored origins
  a change to one row can invalidate the guard on any number of later ones. The
  whole-list `PUT` is unaffected and becomes the safer write.

## Alternatives considered

### ADR 0003's cascade, unchanged

Strand everything after a failure, and accept that a fallback cannot be
expressed. This is simpler to state and it is strictly less capable: the two
rules agree on every continuation and differ only where the player wrote an
origin the cascade cannot read. Rejected because the extra capability costs
nothing to implement — it is the same comparison — and because the cascade has
to keep a positional rule that the guard does not need.

### Guard every order, without exception

The first draft of this ADR put an origin on every order, including a rest, so
that the rule would have no exceptions and no per-kind table. It was rejected
because the uniformity is false economy: the table exists anyway in everything
else a kind declares, and guarding unconditionally removes the player's ability
to say "do this wherever I end up" — which for a rest is almost always what
they mean.

### A conditional order kind

`Move E, else Move SE` as a single order with two directions in it. This is
explicit, it cannot go stale, and a reader does not have to work out what an
origin implies. It was rejected because it expresses exactly one level of
fallback and nothing beyond it: the four-row example above has no spelling as a
pair of conditionals, and a second level would need either nesting or a new
kind again. It also adds an order kind, a detail table, and validation for
something the origin already says.

### Derived origins plus a fallback flag

Keep the server computing the hexes, and let a player mark a row as an
alternative to the row before it. This avoids stale guards entirely, because
nothing is authored. It was rejected because the flag is a second way of saying
what the origin says, and because "alternative to the row before it" cannot
express a fallback to a row further back, which the origin can.

### Re-anchor the tail, as the executor does today

Keep walking the remaining orders from wherever the entity actually is. This is
current behaviour and is rejected for the reasons ADR 0003 gives: a route
re-anchored one hex off is not a worse version of the player's plan, it is a
different plan, and it reports itself as `Carried out`.
