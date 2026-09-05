# Map view reference

## Routes

| Route | Role | Page |
| --- | --- | --- |
| `GET /admin/map` | `admin` | The true map |
| `GET /player/map` | `player` | The account's own map |

A request without a valid session is directed to `/sign-in`. A request whose
account holds the other role is directed to that account's dashboard. A player
whose required faction metadata is incomplete is directed to `/player/faction`.

Each dashboard links to the map its role may view. Each map links back to that
dashboard.

## Viewport

The pages have no pan, zoom, or scroll, so what each map draws is the entire
viewport.

| Map | Center | Extent |
| --- | --- | --- |
| Admin | The game origin `(0, 0, 0)` | The whole world |
| Player | The account origin hex | Radius 6 |

A disc of radius `n` contains `3n(n + 1) + 1` hexes. The world's radius is given
in [Terrain reference](terrain.md).

## Admin map

Every hex of the world is drawn with its terrain. Coordinates are true axial
`(q, r)` values.

## Player map

Hexes the account can see are drawn with their terrain. Every other hex in the
disc is drawn as fog: the hex outline, no terrain. A hex outside the world is
drawn as fog even when it is in the visible set, so the edge of the world is not
distinguishable from unexplored ground.

Coordinates are the account's own axial `(q, r)` values, in which the account
origin hex is `(0, 0)`. The page contains no true map coordinate.

## Coordinate frames

An account with true origin hex `O` and rotation `k` displays a true coordinate
`t` at the coordinate `p` on its own map:

```text
p = rotate_left^k (t - O)
t = O + rotate_right^k (p)
```

Rotation is the cube rotation about `(0, 0, 0)`, applied after the translation.
It is a property of the account, not of the orientation the map is drawn in.

The main admin has the game origin and rotation `0`, so its own map and the true
map are the same. See [Player origin reference](player-origin.md) for the
placement and rotation rules.

## Visible hexes

The visible set holds the true coordinates an account can see. It is not a
record of the hexes an account has visited; an account may see a hex it has not
entered.

An account currently sees one hex: its origin. No table records visibility, and
the game has no turn.

## Terrain

Map terrain and elevation are read from the `hexes` table, which holds the world
generated when the database was created. Drawing a map writes no rows. See
[Terrain reference](terrain.md).

## Rendering

Each map is one inline SVG element in the page.

- The grid is flat-top, per [Product reference](../PRODUCT.md).
- One `polygon` element is drawn per hex. Its class is the terrain value, or
  `fog` when the hex is not visible.
- Each `polygon` carries a `title` giving the hex coordinate in that page's
  frame, and either its terrain and elevation or `unexplored`. Elevation is
  given as `N m` on land and `N m deep` in water.
- The `viewBox` is computed from the drawn hexes and bounds the whole map, so
  the map scales to its container.
- Hexes are drawn in a fixed order, so identical requests produce identical
  markup.
