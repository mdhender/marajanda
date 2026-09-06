# Orders reference

What a faction tells its entities to do in a turn, how those orders are stored,
and what the pages that build them accept.

Implemented by `internal/game` (`order.go`), `internal/datastore` (`order.go`),
and `internal/server` (`orders.go`). See
[#29](https://github.com/mdhender/marajanda/issues/29).

## Vocabulary

| Term | Definition |
| --- | --- |
| Order | One instruction issued to one entity for one turn. Also called a stanza. |
| Order kind | What an order tells an entity to do. `move` is the only kind. |
| Sequence | An order's position in its entity's list for the turn. Contiguous from 1. |
| Step | One direction of a move, at a position in that order. Contiguous from 1. |

An order is issued to an entity, not to a faction. The faction is reached
through the entity, so a faction with two leaders says which one is moving.

## Order kinds

| Kind | Meaning |
| --- | --- |
| `move` | Walk the entity one hex per step, in the order the steps are given. |

Which kinds an entity accepts is a function of its kind:

| Entity kind | Accepts |
| --- | --- |
| `leader` | `move` |
| `hamlet` | Nothing |

`game.EntityKind.OrderKinds` lists them and `game.EntityKind.Accepts` reports
one. The form offers only the kinds an entity accepts, and the datastore refuses
the rest, so a hand-built request cannot do what the form declines to show.

## Directions

A step's direction is one of the six compass points, stored as the lowercase
abbreviation: `ne`, `e`, `se`, `sw`, `w`, `nw`. That is what `compass.Parse`
accepts and what `strings.ToLower` of a point produces, matching how terrain and
race are stored. `compass.Point`'s zero value is not a point, so a box that was
never filled in cannot become north-east. See
[Compass reference](compass.md).

## Storage

| Table | Columns | Primary key |
| --- | --- | --- |
| `orders` | `turn`, `entity_id`, `seq`, `kind` | `(turn, entity_id, seq)` |
| `order_steps` | `turn`, `entity_id`, `seq`, `step`, `direction` | `(turn, entity_id, seq, step)` |

An order's directions are a list, so `move nw ne e` is three rows in
`order_steps` and a blank box is the absence of a row. `order_steps` references
`orders` and cascades from it; `orders` references `entities` and cascades from
that. The second order kind brings its own detail table rather than widening a
fixed-slot row.

`step` is constrained to `1 .. 32`. That bound is a storage sanity limit, not a
game rule: the movement allowance is decided by turn processing. It is written
into the schema from `datastore.MaxOrderSteps`, so the column check and the code
that has to satisfy it read one value.

Column definitions are in [Datastore](../DATASTORE.md#orders).

## Numbering

Sequences and steps are contiguous, and every write leaves them that way:

- Removing an order renumbers the ones after it, so a list of three orders with
  the first removed is numbered 1 and 2.
- Clearing a step in the middle of an order shifts the later ones left, so
  `move nw <blank> e` is stored and re-rendered as `move nw e`.

One order therefore has exactly one stored form, which is what a replay depends
on.

## History

Only the current turn's rows are writable. `datastore` refuses every insert,
update and delete where the turn is not `game.current_turn`, whatever turn a
caller asks for. Advancing the turn is what freezes the turn before it, and
nothing deletes an order from a turn the game has moved past.

A replay is therefore: regenerate the world from the stored seeds and
dimensions, apply turn 1's orders in `(entity, seq, step)` order, then turn 2,
and so on. Entity ids are identity, not randomness: a rule needing per-entity
randomness keys on values recorded in history, never on the id. See
`internal/prng/doc.go` and [Entities reference](entities.md).

Deleting an account erases its orders through the cascade. Nothing deletes
accounts.

## Store methods

| Method | Effect |
| --- | --- |
| `OrdersAsOf(ctx, email, turn)` | The faction's orders on `turn`, keyed by entity, in sequence order. |
| `AddOrder(ctx, email, turn, entity, kind)` | Appends an order and returns its sequence number. |
| `SetOrderStep(ctx, email, turn, entity, seq, step, direction)` | Sets one step box. An invalid direction clears it. |
| `SetOrderSteps(ctx, email, turn, stanzas)` | Replaces the steps of every named order, in one transaction. |
| `RemoveOrder(ctx, email, turn, entity, seq)` | Removes an order and renumbers the rest. |
| `AdvanceTurn(ctx)` | Moves the clock on by one and returns the new turn. |

Every write is refused with a sentinel error a caller can tell apart:

| Error | Condition |
| --- | --- |
| `ErrTurnClosed` | The turn is not the one the game is on. |
| `ErrUnknownEntity` | The entity is not the faction's, or did not stand in the world on the turn. |
| `ErrOrderKindRefused` | The entity's kind does not accept that order kind. |
| `ErrUnknownOrder` | The entity has no order with that sequence number. |
| `ErrUnknownStep` | The step is outside the boxes the order shows. |
| `ErrTooManySteps` | The order would carry more than `MaxOrderSteps` steps. |
| `ErrFactionInactive` | The faction has been deactivated. |

`SetOrderStep` addresses a box the page showed: one of the stored steps, or the
blank box on the end, which is the step count plus one. Choosing a direction
there appends a step; the blank option there changes nothing.

## Routes

| Route | Role | Response |
| --- | --- | --- |
| `GET /player/orders` | `player` | The orders page, or the orders region alone |
| `POST /player/orders` | `player` | Saves every step box the form carries, then adds or removes if a button says so |
| `POST /player/orders/{entity}/{seq}/{step}` | `player` | Sets one step box |
| `DELETE /player/orders/{entity}/{seq}` | `player` | Removes one order |
| `POST /admin/turn` | `admin` | Advances the turn and returns to the admin dashboard |

A request without a valid session is directed to `/sign-in`. A request whose
account holds the other role is directed to that account's dashboard. A player
whose required faction metadata is incomplete is directed to `/player/faction`.
The player dashboard links to the orders page. Admins have no orders page.

A player whose faction is deactivated is directed to `/player/dashboard` from
every one of the four orders routes, not to `/player/faction`: the faction is
configured, and the form would ask for a faction the player already has. That
dashboard says the faction is not active and omits the link to the orders page.
Everything else the player has stays reachable, including the map and sign-out.

The store refuses the writes as well as the page. A hand-built request that
reaches one is answered `403` with `ErrFactionInactive`, which is the rule
order legality already follows: a request cannot do what the form declines to
show. An account that should be shut out entirely is deactivated on the
account; see [Accounts reference](../ACCOUNTS.md#deactivation).

`POST /admin/turn` increments `game.current_turn` and nothing else. Processing
the orders of the turn it closes is separate work.

## Form fields

| Field | Value |
| --- | --- |
| `step.<entity>.<seq>.<step>` | A direction abbreviation, or empty for a box with nothing in it |
| `kind.<entity>` | The order kind an entity's add control would append |
| `add` | The entity to append an order to, set by the add button |
| `remove` | The `<entity>.<seq>` to remove, set by a remove button |

A step box carries its whole address in its name because one control serves two
pages. With script, the box posts itself to the URL that names the same box.
Without script, every box is submitted together by the page's one Save button,
and the name is the only thing that says which box a value belongs to. HTMX
includes the whole enclosing form on a non-`GET` request, so a scripted write
carries every other box with it and the URL is what says which one was touched.

## Page

The page names the turn, holds a faction picker, and lists one section per
entity the faction owns, in the order the force is listed. An entity whose kind
accepts no order kinds is listed with "No orders available yet", so a player sees
their whole force in one place.

Each order shows its kind, one `<select>` per stored step plus one blank
`<select>` on the end, and a remove control. Below an entity's orders, an add
control offers the kinds that entity accepts. The box count is always steps plus
one, so there is no "add a box" control.

The faction picker holds one entry and it is selected. A player commands one
faction; the picker is there so the page has a stable shape.

Every write returns the whole re-rendered `#orders` region rather than the
control that was touched, so numbering, compaction and validation are decided by
the server. A scripted write is answered `200` whether or not it was refused,
because HTMX does not swap the response of a failed request and an error nobody
sees is not an error message; the region carries a saved-at line or the refusal.
An unscripted write is answered with a redirect when it lands and with the whole
page and a `4xx` status when it does not.

### Saving

With script, each `<select>` carries `hx-post` and `hx-trigger="change"`, and
every change is saved as it is made. Without script, the page carries one Save
button inside `<noscript>` that submits every box at once. The form's first
submit button saves, so pressing Enter in a box saves rather than pressing the
first Remove on the page.

`<noscript>` does not cover scripting that is enabled but blocked. The content
security policy is `script-src 'self'` with no inline script, so it is the only
script-free detector available.
