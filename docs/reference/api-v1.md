# REST API v1 reference

The versioned JSON interface for agents and browser applications. The active
HTMX interface and this API are two transports over the same datastore and game
behavior. Neither calls the other.

Implemented by `internal/server`. API v1 and the HTMX interface are both active
and maintain capability parity. EmberJS is a prospective API client, not part
of the current application.

The request and response examples below use the seeded test accounts and DTOs
exercised by the server integration tests. Coordinate and terrain values depend
on the configured game seeds, so clients must treat those shown as examples
rather than fixed world data.

## Base path and media type

Every route in this reference starts with `/api/v1`. `GET /api/healthz` remains
unversioned, returns `204 No Content`, and is not an authenticated game
resource.

Requests with a JSON body carry `Content-Type: application/json`. A request
body with another media type is refused with `415 Unsupported Media Type` and
`unsupported_media_type`. Successful representations and every error carry
`Content-Type: application/json`. A `204 No Content` response has no body and
no content type.

Every orders representation carries an `ETag`, and an order write may carry
`If-Match` to say which list it believed it was writing to. See
[Concurrent edits](#concurrent-edits).

Query parameter names are case-sensitive camel case, the same spelling as the
JSON properties, so a value a client reads out of a response body goes back into
a URL under the name it already knows. Path segments are lower case. Error codes
are the one snake case name in the API, which keeps a code distinguishable from a
parameter at a glance. Unlike JSON objects, a query string is not closed: an
unrecognized parameter is ignored, except where this document says a route
refuses one by name.

JSON property names are case-sensitive camel case. Request objects are closed:
an unknown property, malformed JSON, an empty body where an object is required,
or anything after the first JSON value is refused with `400 Bad Request` and
`invalid_json`. Missing or invalid known values are refused with
`invalid_request` or the narrower code listed below.

Collections are JSON arrays, including when empty. Integers are JSON numbers.
Coordinates are true axial coordinates:

```json
{"q":2,"r":-1}
```

## Authentication

A session is one opaque token accepted through either of these credential
sources:

```http
Cookie: marajanda_session=<token>
Authorization: Bearer <token>
```

One request uses at most one source. A request carrying both is refused with
`400 Bad Request` and `credentials_conflict`, even when the two token strings
are equal. A missing, malformed, unknown, or revoked credential is refused with
`401 Unauthorized` and `authentication_required`.

The cookie is host-only, `Secure`, `HttpOnly`, `SameSite=Lax`, and has no
persistent expiration. The raw token is returned only when a session is created
and in that cookie. Storage holds its cryptographic hash, never the token.
A session has no idle or absolute timeout in v1. It remains valid until it is
revoked or its account is deleted. Deactivating an account prevents a new
session but does not revoke one the account already holds.

Unsafe cookie-authenticated requests remain subject to the server's cross-origin
protection. Bearer authentication does not establish a CORS policy. The API is
for same-origin browser deployment and non-browser agents; it sends no
cross-origin access headers.

For a bearer client, create a session, retain the `token` property from the
response, and send it on later requests:

```http
POST /api/v1/sessions HTTP/1.1
Host: marajanda.example
Content-Type: application/json

{"email":"player@marajanda.com","passphrase":"good.luck"}
```

```http
GET /api/v1/account HTTP/1.1
Host: marajanda.example
Authorization: Bearer <token>
```

A same-origin browser may retain the `Set-Cookie` value from session creation
instead. It then sends the cookie without also sending `Authorization`:

```http
GET /api/v1/account HTTP/1.1
Host: marajanda.example
Cookie: marajanda_session=<token>
```

For a persistent database, either credential remains valid after the server is
stopped and reopened. Restarting does not rotate or expire sessions. Raw tokens
are credentials: do not log, publish, or persist them where an unauthorized
reader can recover them. The datastore cannot recover a raw token from its
stored SHA-256 hash.

## Errors

Every error has one envelope. `message` is suitable for a person but is not a
stable matching surface; clients branch on `code` and HTTP status.

```json
{
  "error": {
    "code": "turn_closed",
    "message": "Only the current turn's orders can be changed."
  }
}
```

| Status | Code | Meaning |
| ---: | --- | --- |
| `400` | `invalid_json` | The required JSON object could not be decoded exactly. |
| `400` | `invalid_request` | A path, query, or known request property is missing or malformed. |
| `400` | `credentials_conflict` | Cookie and bearer credentials were both supplied. |
| `401` | `credentials_rejected` | The email/passphrase pair was not accepted. |
| `401` | `authentication_required` | A protected route has no valid session. |
| `403` | `account_inactive` | Correct credentials belong to a deactivated account. |
| `403` | `forbidden` | The authenticated role cannot use the resource. |
| `403` | `faction_inactive` | The player's configured faction may be viewed but cannot issue orders. |
| `404` | `faction_not_configured` | The player has not configured a faction. |
| `404` | `entity_not_found` | The entity is not one of the player's faction entities. |
| `404` | `order_not_found` | The addressed order or insertion position does not exist. |
| `404` | `turn_not_found` | A turn-scoped read names a turn the game has not reached. |
| `409` | `turn_closed` | An order write names a turn that is no longer current. |
| `412` | `precondition_failed` | An order write's `If-Match` no longer describes the stored orders. |
| `409` | `no_origin` | Faction configuration cannot find a valid origin in this world. |
| `415` | `unsupported_media_type` | A route requiring JSON did not receive `application/json`. |
| `422` | `order_refused` | The entity refuses the kind or the detail is not valid for the order. |
| `422` | `order_limit` | The write would exceed the per-entity order limit. |
| `500` | `internal_error` | The server could not complete an otherwise valid request. |

An unexpected error never includes its internal error text in `message`.

## Routes

| Method and path | Role | Success | Capability |
| --- | --- | ---: | --- |
| `POST /api/v1/sessions` | Public | `201` | Authenticate and create a session. |
| `DELETE /api/v1/session` | Either | `204` | Revoke the presented session. |
| `GET /api/v1/account` | Either | `200` | Read the current account and client-routing state. |
| `GET /api/v1/game` | Either | `200` | Read dimensions and the current turn; admins also receive seeds. |
| `GET /api/v1/faction` | Player | `200` | Read the player's configured faction. |
| `PUT /api/v1/faction` | Player | `200` | Configure or rename the player's faction. |
| `GET /api/v1/entities` | Player | `200` | Read the player's entities as of the reported turn. |
| `GET /api/v1/map` | Either | `200` | Read all hexes as admin or visible hexes as player. |
| `GET /api/v1/orders` | Player | `200` | Read and estimate current orders. |
| `GET /api/v1/results` | Player | `200` | Read what the latest processed turn did. |
| `POST /api/v1/entities/{entity}/orders` | Player | `201` | Append or insert an order. |
| `PUT /api/v1/entities/{entity}/orders` | Player | `200` | Declare an entity's whole order list. |
| `PATCH /api/v1/entities/{entity}/orders/{sequence}` | Player | `200` | Set one order's detail. |
| `PUT /api/v1/orders` | Player | `200` | Set multiple order details atomically. |
| `DELETE /api/v1/entities/{entity}/orders/{sequence}?turn={turn}` | Player | `200` | Remove and renumber an order. |
| `POST /api/v1/turns/current/advance` | Admin | `200` | Process and advance the current turn. |
| `GET /api/v1/turns/{turn}/entities` | Player | `200` | Read the player's entities as of a turn. |
| `GET /api/v1/turns/{turn}/orders` | Player | `200` | Read and estimate orders as of a turn. |
| `GET /api/v1/turns/{turn}/map` | Either | `200` | Read the map as of a turn. |
| `GET /api/v1/turns/{turn}/results` | Player | `200` | Read what a turn did. |

Path values `entity`, `sequence`, and the delete query value `turn` are positive
decimal integers. Invalid syntax is `invalid_request`. An absent `turn` on the
delete route is also invalid; every order write names the turn the client read,
so advancing the clock makes a stale write a `turn_closed` conflict instead of
silently applying it to the new turn.

`turn` on the delete route names the turn the client read. It does not select a
snapshot, and no read route accepts it with that meaning. A turn is selected by
path, not by query: `GET /api/v1/entities` and `GET /api/v1/turns/{turn}/entities`
are the same read reaching its turn two ways. The unscoped reads therefore refuse
`asOfTurn`, `asOf`, and `turn` with `400 Bad Request` and `invalid_request`
rather than answering the current turn under a name the client meant as the past.
Were a query spelling ever added, it would be `asOfTurn`.

## Sessions

### Create

`POST /api/v1/sessions` takes the identifier and passphrase used by the sign-in
form:

```json
{
  "email": "player@marajanda.com",
  "passphrase": "good.luck"
}
```

Email normalization and credential rejection are the rules in
[Accounts](../ACCOUNTS.md). Success sets the session cookie and returns the raw
token once with the current account:

```json
{
  "token": "<opaque token>",
  "account": {
    "email": "player@marajanda.com",
    "handle": "player",
    "role": "player",
    "active": true,
    "seated": true,
    "origin": {"q": 2, "r": -1},
    "factionConfigured": true
  }
}
```

### Delete

`DELETE /api/v1/session` revokes the token used to authenticate that request,
expires the cookie whether authentication came from cookie or bearer, and
returns `204 No Content`. Reusing the token is then `authentication_required`.

## Current account

`GET /api/v1/account` returns the `account` object shown above. `origin` is an
axial coordinate when `seated` is true and JSON `null` otherwise.
`factionConfigured` is false for every assistant admin and for a player that
must configure a faction; it is true for the main admin and a configured
player. It is client-routing state, not an authorization grant.

## Game

`GET /api/v1/game` returns the current turn and the stored half-extents. The
world is `2 * width + 1` columns by `2 * height + 1` rows.

```json
{
  "currentTurn": 3,
  "width": 255,
  "height": 127
}
```

An admin response additionally contains the two signed 64-bit seeds, in order:

```json
"seeds": [98374, -98]
```

A player response omits `seeds`; it is never `null` or an empty array. This is
the same role boundary as the dashboards.

## Faction

`GET /api/v1/faction` returns a configured player's faction:

```json
{
  "name": "The Wayfarers",
  "race": "human",
  "active": true,
  "configured": true
}
```

An unconfigured player receives `faction_not_configured`. An inactive faction
is returned with `active: false`; inactivity limits order writes, not reads.

`PUT /api/v1/faction` takes:

```json
{"name":"The Wayfarers","race":"human"}
```

It uses the same normalization, race list, seating, founding, and transaction
as the faction form. Success returns the faction representation above. Repeating
the request reconfigures the existing faction according to the current product
rules; it does not found new entities or move the origin.

## Entities

`GET /api/v1/entities` reads every entity of the player's faction as of one
turn. `turn` and all entity facts in the response describe the same snapshot.

```json
{
  "turn": 3,
  "entities": [
    {
      "id": 1,
      "code": "LEADER-1",
      "name": "LEADER-1",
      "kind": "leader",
      "location": {"q": 2, "r": -1},
      "allowance": 6,
      "orderKinds": ["move", "rest"]
    }
  ]
}
```

`allowance` is zero for an entity with no allowance row. `orderKinds` is the
game rule for the entity's kind and is empty when the entity takes no orders.

The turn is the current one. See [Past turns](#past-turns) for reading an
earlier one.

## Map

`GET /api/v1/map` returns a role-filtered map in deterministic world order. An
admin receives every stored hex. A player receives only the coordinates the
faction knew on the reported turn, with terrain and elevation read from the
stored world. Observed
and explored hexes have the same representation because the active player map
does not distinguish them. Unknown and fog hexes are omitted rather than
represented with hidden properties.

```json
{
  "turn": 3,
  "width": 255,
  "height": 127,
  "hexes": [
    {
      "coordinate": {"q": 2, "r": -1},
      "terrain": "grassland",
      "elevation": 82
    }
  ]
}
```

`width` and `height` are the stored half-extents. `turn` is the turn against
which player visibility was read; it is also present for admins so the resource
shape is stable across roles, though no turn changes what an admin sees. See
[Past turns](#past-turns) for reading an earlier one. The endpoint does not return SVG, PNG,
window controls, fog geometry, or game seeds.

## Orders

### Detail

An order is one action. Its `detail` has exactly the property its kind carries:

```json
{"sequence":1,"kind":"move","detail":{"direction":"ne"}}
{"sequence":2,"kind":"rest","detail":{"count":2}}
```

A newly added move may have no selected direction and is represented by an
empty detail object. A rest always has `count` from 1 through 32. A request that
supplies both properties, supplies the wrong property for its kind, or supplies
an invalid value is `order_refused`.

### Read and estimate

`GET /api/v1/orders` returns one entry for every entity shown on the orders
page, including entities that accept no orders. Orders and estimates are read
for the reported current turn; see [Past turns](#past-turns) for reading an
earlier one, estimate included.

```json
{
  "turn": 3,
  "entities": [
    {
      "entityId": 1,
      "orders": [
        {"sequence": 1, "kind": "move", "detail": {"direction": "ne"}}
      ],
      "estimate": {
        "allowance": 6,
        "committed": 3,
        "total": 3,
        "residue": 3,
        "overspend": 0,
        "exhaustsAt": null,
        "end": {"q": 3, "r": -2},
        "orders": [
          {
            "sequence": 1,
            "kind": "move",
            "cost": 3,
            "running": 3,
            "exhausts": false,
            "warning": null,
            "from": {"q": 2, "r": -1},
            "target": {"q": 3, "r": -2},
            "to": {"q": 3, "r": -2}
          }
        ]
      }
    }
  ]
}
```

`cost` is JSON `null` when an incomplete move cannot be priced.
`exhaustsAt` is JSON `null` when every order fits. Estimates are fogged exactly
as on the player orders page; they do not disclose actual unknown terrain.
`residue` is what the allowance leaves unspent: the entity's idle action
points. It is a number and never an order, so nothing appears in `orders` that
the player did not write, and nothing rests an entity that was not ordered to.

A client that wants those points spent resting appends a rest of that length
through `POST /api/v1/entities/{entity}/orders`. That is the whole of what the
orders page's "Rest the remaining N points" control does on the player's behalf,
so it needs no operation of its own; see
[Action points reference](action-points.md#resting-the-idle-points).

`warning` is what the pre-processor expects to stop the order, or `null` when it
expects nothing to. It uses the vocabulary a turn result reports afterwards, so
"what I was told" and "what happened" read as the same word.

It is advice and not a verdict. The order is still priced, `to` still names
where the step points, and the walk still goes on from there, because every
order is assumed to land and an estimate that refused a step would mis-price
every order after it the moment something the pre-processor does not model got
the entity across. A warning that turns out wrong is a warning; a refusal that
turns out wrong is a lie.

The pre-processor speaks about two things only: a coordinate the world does not
have, whose extent is published in `GET /api/v1/game` rather than hidden, and
impassable ground the faction already knows. It is silent about ground the
faction has not seen, because saying anything would disclose what is there.
That case is carried by the flat exploration price instead - a real cost rather
than a warning.

### Append or insert

`POST /api/v1/entities/{entity}/orders` takes:

```json
{
  "turn": 3,
  "kind": "move",
  "detail": {"direction": "ne"}
}
```

Omitting `sequence` appends to the end of the list. Supplying `sequence`
inserts the new order at that one-based position and shifts that position and
the following orders up:

```json
{
  "turn": 3,
  "sequence": 2,
  "kind": "rest",
  "detail": {"count": 1}
}
```

Success returns the affected entity's complete authored orders and refreshed
estimate:

```json
{
  "turn": 3,
  "entityId": 1,
  "sequence": 2,
  "orders": [
    {"sequence": 1, "kind": "move", "detail": {"direction": "ne"}},
    {"sequence": 2, "kind": "rest", "detail": {"count": 1}}
  ],
  "estimate": {
    "allowance": 6,
    "committed": 4,
    "total": 4,
    "residue": 2,
    "overspend": 0,
    "exhaustsAt": null,
    "end": {"q": 3, "r": -2},
    "orders": [
      {
        "sequence": 1,
        "kind": "move",
        "cost": 3,
        "running": 3,
        "exhausts": false,
        "from": {"q": 2, "r": -1},
        "target": {"q": 3, "r": -2},
        "to": {"q": 3, "r": -2}
      },
      {
        "sequence": 2,
        "kind": "rest",
        "cost": 1,
        "running": 4,
        "exhausts": false,
        "from": {"q": 3, "r": -2},
        "target": {"q": 3, "r": -2},
        "to": {"q": 3, "r": -2}
      }
    ]
  }
}
```

### Declare a whole list

`PUT /api/v1/entities/{entity}/orders` replaces the entity's orders for the turn
with the list it is given. It is the write a client can retry.

```json
{
  "turn": 3,
  "orders": [
    {"kind": "move", "detail": {"direction": "ne"}},
    {"kind": "rest", "detail": {"count": 2}}
  ]
}
```

An order here carries no `sequence`: its position in `orders` is its sequence.
That is what makes the request idempotent - sending it again asks for the same
list rather than for a second copy of it - and it is why a client whose
connection dropped mid-write can send it again instead of reading back to find
out what landed. `POST` cannot do this: an append that may or may not have
happened cannot be repeated.

Every order is checked the way one appended order is, and nothing is written
unless all of it can be. An absent `orders` is `invalid_request`; an empty
`orders` is a declaration that the entity has no orders this turn, which no
other route can say in one request.

Success returns the affected entity's complete orders and refreshed estimate.
`sequence` is absent from that response, because a whole-list write addresses no
one order.

### Set one detail

`PATCH /api/v1/entities/{entity}/orders/{sequence}` takes the turn and the new
complete detail:

```json
{"turn":3,"detail":{"direction":"nw"}}
```

It does not change the stored order kind. Success returns the same refreshed
affected-entity representation as append or insert.

### Set details atomically

`PUT /api/v1/orders` is the script-free form's batch operation expressed as
JSON. Every addressed detail is applied in one transaction or none is.

```json
{
  "turn": 3,
  "updates": [
    {"entityId": 1, "sequence": 1, "detail": {"direction": "nw"}},
    {"entityId": 4, "sequence": 2, "detail": {"count": 2}}
  ]
}
```

Success returns the complete `GET /api/v1/orders` representation after the
write, including all affected estimates.

### Remove

`DELETE /api/v1/entities/{entity}/orders/{sequence}?turn=3` removes the order
and renumbers those after it. Success returns the affected entity's complete
orders and refreshed estimate; `sequence` in that response is the removed
sequence.

## Turn results

`GET /api/v1/results` returns the account of one processed turn: what every
entity of the faction was allowed, what its orders were charged, what each order
did, and what each order revealed. The vocabulary is the one
[Turn results reference](turn-results.md) fixes, and the three grains it records
on stay three in the response.

```json
{
  "turn": 3,
  "entities": [
    {
      "entityId": 1,
      "ledger": {
        "allowance": 6,
        "spent": 4,
        "lapsed": 2,
        "start": {"q": 2, "r": -1},
        "end": {"q": 4, "r": -1}
      },
      "orders": [
        {
          "sequence": 1,
          "kind": "move",
          "cost": 1,
          "carried": true,
          "reason": null,
          "from": {"q": 2, "r": -1},
          "target": {"q": 3, "r": -1},
          "to": {"q": 3, "r": -1}
        },
        {
          "sequence": 2,
          "kind": "move",
          "cost": 1,
          "carried": false,
          "reason": "terrain",
          "from": {"q": 3, "r": -1},
          "target": {"q": 4, "r": -2},
          "to": {"q": 3, "r": -1}
        }
      ],
      "observations": [
        {"sequence": 1, "coordinate": {"q": 3, "r": -1}, "state": "explored"},
        {"sequence": 2, "coordinate": {"q": 4, "r": -2}, "state": "observed"}
      ]
    }
  ]
}
```

`cost` is what the entity was charged, not what the order would have cost: an
order it could not afford charges nothing, and a step that failed on terrain is
charged in full. `reason` is `null` for a carried order and otherwise one of
`terrain`, `exhaust`, `blocked`, or `unknown`. `from`, `target`, and `to` are all
three recorded because a step that fails does not move the entity, so the order
after it resolves from the hex it did not leave.

`observations` are of the entity and carry the `sequence` of the order that
revealed each hex, rather than being nested inside that order. The record keeps
the grains apart, a client that wants them nested groups by `sequence`, and one
hex may be revealed twice in a turn - observed by one step and explored by a
later one - with both sightings kept.

Results are read, never derived again. A past turn returns the same bytes
however far the game moves on.

### Which turn the unscoped read answers

`GET /api/v1/results` answers with the **latest processed turn**, which is the
turn before the current one. It is the one read in this API where unscoped does
not mean current, and the resource is what makes it so: results are written by
turn processing, so the current turn has none until it is closed.

| Game state | `GET /api/v1/results` |
| --- | --- |
| At least one turn processed | `200` with that turn's report and its `turn` |
| No turn processed yet | `200` with `"turn": 0` and an empty `entities` |

Turn `0` is the "before the game's first turn" sentinel, so a client reads "no
turn has been reported" from the turn rather than from an empty list it has to
interpret. `GET /api/v1/turns/current/results` is still there for a client that
means today, and answers with today: an empty collection until the turn is
processed.

## Advance the turn

`POST /api/v1/turns/current/advance` takes no body. It processes the orders of
the turn it closes and increments the game clock in the same transaction as the
admin form. Success returns the new turn:

```json
{"turn":4}
```

## Past turns

`GET /api/v1/turns/{turn}/entities`, `GET /api/v1/turns/{turn}/orders`,
`GET /api/v1/turns/{turn}/map`, and `GET /api/v1/turns/{turn}/results` are the
unscoped reads of the same names with the turn named in the path. Each returns the representation that read returns, and
`turn` in the body is the turn asked for. `/api/v1/entities` is
`/api/v1/turns/{current}/entities`; the unscoped route is the convenience, not
the other way round.

`{turn}` is a positive decimal integer or the literal `current`. `current` is
spelled out so reading today is one request rather than a read of the clock and
a read of the data with an advance possible between them.

| Turn | Answer |
| --- | --- |
| `current`, or `1` through the current turn | `200` with that turn's snapshot |
| A turn the game has not reached | `404` and `turn_not_found` |
| Not a positive decimal integer | `400` and `invalid_request` |

A turn before the player's faction was configured is a `200` with an empty
collection. The faction did not exist, and that is the true account of it rather
than a refusal.

The world a report describes never changes underneath the report, so these reads
are stable: a past turn returns the same bytes however far the game moves on.
What each read holds fixed at the named turn is what that turn recorded -
entity locations and allowances, the stored orders, and the hexes the faction
knew. Terrain and world shape are generated once and never change, so they are
not turn-scoped facts.

An `estimate` on a closed turn is the estimate the player would have been shown
while the turn was open. It is priced against that turn's knowledge, locations
and allowances, so it reproduces what was on screen when the orders were
written - which is the question a client asking about a closed turn is really
asking. It remains an estimate and not a record of what happened; the executor
is what decides that, and [Turn results](#turn-results) is where it is read.

Results are the exception to "unscoped means current": `/api/v1/results` is
`/api/v1/turns/{current-1}/results`, because the current turn has no results
until it is processed. See
[Which turn the unscoped read answers](#which-turn-the-unscoped-read-answers).

## Concurrent edits

Two clients can hold the same turn's order list - the orders page in one tab and
a script in another is ordinary for a play-by-mail game - and an append tells
neither that the other has written. `ETag` and `If-Match` are how a client finds
out.

Every response carrying an orders representation carries an `ETag`: the reads at
`GET /api/v1/orders` and `GET /api/v1/turns/{turn}/orders`, and the response to
every order write. The tag on a write response is the tag of the list that write
just made, so a client writing several times in a row never has to read between
them.

The tag is derived from the orders themselves, not stored beside them, and is
computed from the list a response has already read rather than by going back for
it. It identifies the whole representation even though it covers only the
orders,
because an estimate is a function of the orders, the entities and what the
faction knows, and within one turn the last two do not move: turn processing
dates everything it writes from turn+1, and a turn the game has left refuses
writes. The turn is part of what is hashed. Nothing about the encoding is
promised; treat a tag as opaque and compare it only for equality.

An order write may carry `If-Match`:

| `If-Match` | Meaning |
| --- | --- |
| absent | Write against whatever is stored. This is what every client did before the header existed. |
| one strong entity-tag | Write only if the stored orders still hash to that tag. |
| `*` | Write if a representation exists at all, which on these routes it does. |

A write whose expectation no longer holds is refused with `412 Precondition
Failed` and `precondition_failed`, and changes nothing. The recovery is to read
the orders again, decide what to do about what changed, and retry with the tag
that read returned.

The comparison happens inside the write's own transaction, so it is a
precondition and not a look before a leap: no other write can land between the
check and the write it guards.

A list of tags is refused with `invalid_request`. A write changes one resource,
so several candidate tags cannot all describe the one it is writing to, and a
server that picked one would be guessing. A weak tag (`W/"..."`) is refused for
the reason the header exists: weak comparison admits representations that
differ, and this comparison decides whether somebody else's orders are about to
be written over.

The HTML orders page carries the same guard by another route: it renders its
list's tag in a hidden field, so every control it offers sends the tag of the
list the player is looking at. A page write that loses a race leaves the list on
screen alone and swaps in a notice above it - the orders the other client wrote
are not orders that player has seen, and arriving underneath their cursor is the
part that would startle - with the controls quiet until Refresh asks for a new
draw.

One thing this does not yet do: `If-None-Match` on the reads is not honoured, so
there is no `304`. The tag is here for writing safely, not for saving bandwidth.
The header also stays optional, so a client that sends no expectation writes
unconditionally, as every client did before it existed.

## Capability parity

| Domain capability | HTML/HTMX | API v1 |
| --- | --- | --- |
| Sign in and out | `/sign-in`, `/sign-out` | sessions |
| Current identity and role | dashboards | account |
| Current game and turn | dashboards and orders | game |
| Configure a faction | `/player/faction` | faction |
| Read player entities | player dashboard and orders | entities, entities as of a turn |
| Read the whole world | admin map and image | admin map representation, as of a turn |
| Read visible terrain | player map | player map representation, as of a turn |
| Read and estimate orders | `/player/orders` | orders, orders as of a turn |
| Read what a processed turn did | `/player/results` | results, results as of a turn |
| Add, insert, edit, batch-save, and remove orders | `/player/orders...` | order mutations, whole-list declare |
| Advance the turn | `/admin/turn` | turn advance |

Parity is a change rule: a change that adds or removes an authenticated UI
capability also adds, removes, or explicitly revises its API representation and
this table in the same change. Parity applies to domain facts, authorization,
validation, and effects. It does not require JSON to reproduce HTML, SVG, PNG,
redirects, or HTMX's fragment-specific status behavior.
