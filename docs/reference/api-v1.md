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
| `409` | `turn_closed` | An order write names a turn that is no longer current. |
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
| `POST /api/v1/entities/{entity}/orders` | Player | `201` | Append or insert an order. |
| `PATCH /api/v1/entities/{entity}/orders/{sequence}` | Player | `200` | Set one order's detail. |
| `PUT /api/v1/orders` | Player | `200` | Set multiple order details atomically. |
| `DELETE /api/v1/entities/{entity}/orders/{sequence}?turn={turn}` | Player | `200` | Remove and renumber an order. |
| `POST /api/v1/turns/current/advance` | Admin | `200` | Process and advance the current turn. |

Path values `entity`, `sequence`, and the delete query value `turn` are positive
decimal integers. Invalid syntax is `invalid_request`. An absent `turn` on the
delete route is also invalid; every order write names the turn the client read,
so advancing the clock makes a stale write a `turn_closed` conflict instead of
silently applying it to the new turn.

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

## Map

`GET /api/v1/map` returns a role-filtered map in deterministic world order. An
admin receives every stored hex. A player receives only coordinates returned by
`VisibleHexes`, with terrain and elevation read from the stored world. Observed
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

`width` and `height` are the stored half-extents. `turn` is the current turn
against which player visibility was read; it is also present for admins so the
resource shape is stable across roles. The endpoint does not return SVG, PNG,
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
for the reported current turn.

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
The trailing Rest is reported as `residue`, not repeated in `orders`, following
the existing preprocessor contract.

### Append or insert

`POST /api/v1/entities/{entity}/orders` takes:

```json
{
  "turn": 3,
  "kind": "move",
  "detail": {"direction": "ne"}
}
```

Omitting `sequence` appends before the trailing Rest. Supplying `sequence`
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

## Advance the turn

`POST /api/v1/turns/current/advance` takes no body. It processes the orders of
the turn it closes and increments the game clock in the same transaction as the
admin form. Success returns the new turn:

```json
{"turn":4}
```

## Capability parity

| Domain capability | HTML/HTMX | API v1 |
| --- | --- | --- |
| Sign in and out | `/sign-in`, `/sign-out` | sessions |
| Current identity and role | dashboards | account |
| Current game and turn | dashboards and orders | game |
| Configure a faction | `/player/faction` | faction |
| Read player entities | player dashboard and orders | entities |
| Read the whole world | admin map and image | admin map representation |
| Read visible terrain | player map | player map representation |
| Read and estimate orders | `/player/orders` | orders |
| Add, insert, edit, batch-save, and remove orders | `/player/orders...` | order mutations |
| Advance the turn | `/admin/turn` | turn advance |

Parity is a change rule: a change that adds or removes an authenticated UI
capability also adds, removes, or explicitly revises its API representation and
this table in the same change. Parity applies to domain facts, authorization,
validation, and effects. It does not require JSON to reproduce HTML, SVG, PNG,
redirects, or HTMX's fragment-specific status behavior.
