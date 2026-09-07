# Orders reference

What a faction tells its entities to do in a turn, how those orders are stored,
and what the pages that build them accept.

Implemented by `internal/game` (`order.go`), `internal/datastore` (`order.go`),
and `internal/server` (`orders.go`). See
[#29](https://github.com/mdhender/marajanda/issues/29) and
[#40](https://github.com/mdhender/marajanda/issues/40).

## Vocabulary

| Term | Definition |
| --- | --- |
| Order | One instruction issued to one entity for one turn, and one action. Also called a stanza. |
| Order kind | What an order tells an entity to do. `move` is the only kind. |
| Sequence | An order's position in its entity's list for the turn. Contiguous from 1. |

An order is issued to an entity, not to a faction. The faction is reached
through the entity, so a faction with two leaders says which one is moving.

An order is one action, so `move nw ne e` is three orders and not one order
carrying three directions. Order entry has no notion of a step: a row on the
page is a whole order, which is what lets the page price each row. *Step* is
kept for one unit of movement in a turn's results, where a move that is carried
out is a step taken.

## Order kinds

| Kind | Meaning |
| --- | --- |
| `move` | Walk the entity one hex, in the direction the order names. |

Which kinds an entity accepts is a function of its kind:

| Entity kind | Accepts |
| --- | --- |
| `leader` | `move` |
| `hamlet` | Nothing |

`game.EntityKind.OrderKinds` lists them and `game.EntityKind.Accepts` reports
one. The form offers only the kinds an entity accepts, and the datastore refuses
the rest, so a hand-built request cannot do what the form declines to show.

## Directions

A move's direction is one of the six compass points, stored as the lowercase
abbreviation: `ne`, `e`, `se`, `sw`, `w`, `nw`. That is what `compass.Parse`
accepts and what `strings.ToLower` of a point produces, matching how terrain and
race are stored. `compass.Point`'s zero value is not a point, so an order that
was never filled in cannot become north-east. See
[Compass reference](compass.md).

## Storage

| Table | Columns | Primary key |
| --- | --- | --- |
| `orders` | `turn`, `entity_id`, `seq`, `kind` | `(turn, entity_id, seq)` |
| `move_orders` | `turn`, `entity_id`, `seq`, `direction` | `(turn, entity_id, seq)` |
| `rest_orders` | `turn`, `entity_id`, `seq`, `count` | `(turn, entity_id, seq)` |

`orders` holds the instruction. What a kind needs beyond its kind lives in that
kind's own detail table, one row per order. A detail table references `orders`
and cascades from it; `orders` references `entities` and cascades from that.

A direction is not a nullable column on `orders`, because such a column would
also have to mean "not applicable to this order kind", and a rest has no
direction. A move with no row in `move_orders` is an order a player has added
and not yet said the direction of; the absence of a row is what the blank select
on the page means.

`rest_orders` is defined ahead of the kind that writes to it: the `kind` check
on `orders` does not admit `rest`, so nothing can put a row in it today. Whether
a rest keeps a repeat count is
[#36](https://github.com/mdhender/marajanda/issues/36).

`seq` is constrained to `1 .. 32`. That bound is how many orders an entity may
carry in a turn: it keeps a tolerated overspend bounded rather than being the
movement allowance, which turn processing decides. It is written into the schema
from `datastore.MaxOrdersPerEntity`, so the column check and the code that has
to satisfy it read one value.

Column definitions are in [Datastore](../DATASTORE.md#orders).

## Numbering

Sequences are contiguous from 1, and every write leaves them that way:

- Removing an order renumbers the ones after it, so a list of three orders with
  the first removed is numbered 1 and 2.
- Inserting an order shifts the ones from that position on up by one, so a list
  can be corrected in the middle without retyping its tail.

An entity's orders therefore have exactly one stored form, which is what a
replay depends on.

Clearing an order's direction is not removing it. The blank option leaves the
order in the list with nothing said about where it goes; the remove control is
what takes a row away.

## History

Only the current turn's rows are writable. `datastore` refuses every insert,
update and delete where the turn is not `game.current_turn`, whatever turn a
caller asks for. Advancing the turn is what freezes the turn before it, and
nothing deletes an order from a turn the game has moved past.

A replay is therefore: regenerate the world from the stored seeds and
dimensions, apply turn 1's orders in `(entity, seq)` order, then turn 2,
and so on. Entity ids are identity, not randomness: a rule needing per-entity
randomness keys on values recorded in history, never on the id. See
`internal/prng/doc.go` and [Entities reference](entities.md).

Deleting an account erases its orders through the cascade. Nothing deletes
accounts.

## Store methods

| Method | Effect |
| --- | --- |
| `OrdersAsOf(ctx, email, turn)` | The faction's orders on `turn`, keyed by entity, in sequence order. |
| `AddOrder(ctx, email, turn, entity, kind, direction)` | Appends an order and returns its sequence number. |
| `InsertOrder(ctx, email, turn, entity, seq, kind, direction)` | Puts an order at `seq` and shifts the rest up. |
| `SetOrderDirection(ctx, email, turn, entity, seq, direction)` | Sets which way one order goes. An invalid direction clears it. |
| `SetOrderDirections(ctx, email, turn, directions)` | Sets the direction of every named order, in one transaction. |
| `RemoveOrder(ctx, email, turn, entity, seq)` | Removes an order and renumbers the rest. |
| `AdvanceTurn(ctx)` | Moves the clock on by one and returns the new turn. |

Every write is refused with a sentinel error a caller can tell apart:

| Error | Condition |
| --- | --- |
| `ErrTurnClosed` | The turn is not the one the game is on. |
| `ErrUnknownEntity` | The entity is not the faction's, or did not stand in the world on the turn. |
| `ErrOrderKindRefused` | The entity's kind does not accept that order kind. |
| `ErrUnknownOrder` | The entity has no order with that sequence number, or an insert named a position past the end of the list. |
| `ErrTooManyOrders` | The entity would carry more than `MaxOrdersPerEntity` orders in the turn. |
| `ErrFactionInactive` | The faction has been deactivated. |

The direction a new order carries may be the blank one. That is what the page's
add and insert controls send: a row appears, and the player says which way it
goes afterwards.

`InsertOrder` takes the position the new order is to occupy, from 1 to one past
the end of the list. One past the end is an append, which is what the control on
the last row asks for.

## Routes

| Route | Role | Response |
| --- | --- | --- |
| `GET /player/orders` | `player` | The orders page, or the orders region alone |
| `POST /player/orders` | `player` | Saves every direction the form carries, then adds, inserts or removes if a button says so |
| `POST /player/orders/{entity}/{seq}` | `player` | Sets which way one order goes |
| `POST /player/orders/{entity}/{seq}/insert` | `player` | Puts a new order after that one |
| `DELETE /player/orders/{entity}/{seq}` | `player` | Removes one order |
| `POST /admin/turn` | `admin` | Advances the turn and returns to the admin dashboard |

A request without a valid session is directed to `/sign-in`. A request whose
account holds the other role is directed to that account's dashboard. A player
whose required faction metadata is incomplete is directed to `/player/faction`.
The player dashboard links to the orders page. Admins have no orders page.

A player whose faction is deactivated is directed to `/player/dashboard` from
every one of the five orders routes, not to `/player/faction`: the faction is
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
| `direction.<entity>.<seq>` | A direction abbreviation, or empty for an order with nothing chosen |
| `kind.<entity>` | The order kind an entity's add and insert controls would create |
| `add` | The entity to append an order to, set by the add button |
| `insert` | The `<entity>.<seq>` to insert after, set by an insert button |
| `remove` | The `<entity>.<seq>` to remove, set by a remove button |

A direction select carries its whole address in its name because one control
serves two pages. With script, the select posts itself to the URL that names the
same order. Without script, every select is submitted together by the page's one
Save button, and the name is the only thing that says which order a value
belongs to. HTMX includes the whole enclosing form on a non-`GET` request, so a
scripted write carries every other select with it and the URL is what says which
one was touched.

There is one kind control per entity, and both the add and the insert controls
read the new order's kind from it.

## Page

The page names the turn, holds a faction picker, and lists one section per
entity the faction owns, in the order the force is listed. An entity whose kind
accepts no order kinds is listed with "No orders available yet", so a player sees
their whole force in one place.

Each order is one row: its kind, one `<select>` for the direction it goes, a
control that inserts another order after it, and a control that removes it. An
order is one action, so a row is one price. Below an entity's orders, an add
control offers the kinds that entity accepts and appends a row to the end.

A row whose select is blank is an order with nothing chosen, not an order on its
way out. Emptying a row leaves it in the list; the remove control is what takes
it away.

The faction picker holds one entry and it is selected. A player commands one
faction; the picker is there so the page has a stable shape.

Every write returns the whole re-rendered `#orders` region rather than the
control that was touched, so numbering and validation are decided by the
server. A scripted write is answered `200` whether or not it was refused,
because HTMX does not swap the response of a failed request and an error nobody
sees is not an error message; the region carries a saved-at line or the refusal.
An unscripted write is answered with a redirect when it lands and with the whole
page and a `4xx` status when it does not.

### Saving

With script, each `<select>` carries `hx-post` and `hx-trigger="change"`, and
every change is saved as it is made. Without script, the page carries one Save
button inside `<noscript>` that submits every select at once. The form's first
submit button saves, so pressing Enter in a select saves rather than pressing
the first Remove on the page.

`<noscript>` does not cover scripting that is enabled but blocked. The content
security policy is `script-src 'self'` with no inline script, so it is the only
script-free detector available.
