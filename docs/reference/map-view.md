# Map view reference

## Routes

| Route | Role | Response |
| --- | --- | --- |
| `GET /admin/map` | `admin` | A page holding a window onto the world |
| `GET /admin/map.png` | `admin` | The whole world as a PNG attachment |
| `GET /player/map` | `player` | A page holding the account's own map |

A request without a valid session is directed to `/sign-in`. A request whose
account holds the other role is directed to that account's dashboard. A player
whose required faction metadata is incomplete is directed to `/player/faction`.

Each dashboard links to the map its role may view. Each map links back to that
dashboard.

## Hex size

A hexagon is drawn with a pixel radius of `24`, giving a hexagon 42 pixels wide
and 48 tall. The size is the same on every map and on every device.

There is no zoom. A map is drawn at its natural size and the browser scrolls it,
so the size of a screen decides how many hexes are on it and nothing else.

## Admin map

### Window

The page draws a window of `40` columns by `26` rows, centred on a hex given by
the query:

| Parameter | Meaning | Default |
| --- | --- | --- |
| `at` | The window's centre as a `q,r` pair | Absent; `q` and `r` are read instead |
| `q` | The axial `q` of the window's centre | `0` |
| `r` | The axial `r` of the window's centre | `0` |

`at` is read first. When it is present `q` and `r` are ignored; when it is
absent they give the centre. The two are read by different rules, described
under Panning and Jumping below.

`q` outside the canonical range wraps back into it. `r` outside the world is
clamped to the nearest pole, and a window that would reach past a pole is drawn
short: rows are the one thing the world does not wrap.

Every hex of the window is drawn with its terrain. Coordinates are true axial
`(q, r)` values.

### Panning

Four links move the window by half of itself: `20` columns east or west, `13`
rows north or south. Consecutive windows therefore overlap by half. A fifth link
returns the window to the game origin.

Each is an ordinary link to `/admin/map` with a new `q` and `r`. Panning is a
new window, which is a new page.

### Jumping

A text box beside the pan links takes a coordinate as `q,r` and centres the
window on that hex. It submits `at` as an ordinary `GET` to `/admin/map`: a jump
is a new window, which is a new page, and needs no script.

Whitespace around either number and around the comma is accepted. A column
outside the canonical range wraps back into it, so a column past the meridian
names a real hex of the cylinder.

Anything else centres the window on the game origin `(0, 0)`, the same hex the
"Back to the origin" link reaches. Input is invalid when it is:

- not two integers separated by one comma, or
- a row outside the world.

A row past a pole is not clamped here. Clamping is the pan links' rule, where
half a window past the ice is still a window that was asked for; a typed row
past a pole is a typo.

The box renders empty on every request. Neither a jump that landed nor one that
was refused is echoed back into it, and the window's own centre reports where
the map is.

### The world image

`GET /admin/map.png` returns the whole world drawn at a pixel radius of `4`, as
an attachment named `marajanda-world.png`. Every hex is drawn in its own colour;
nothing is sampled, collapsed or summarized. The default world is one
`3544 x 1532` image.

## Player map

### Window

The window is the smallest rectangle holding every hex the account can see,
grown by a margin of `2` hexes on all four sides. An account that can see one
hex is sent a five-by-five map.

The margin is the same on every side and is never reduced. A hex beyond a pole
is inside the window like any other and is drawn as fog, so the size and shape
of the map are the same wherever in the world the account stands.

There is no pan control, and the route takes no window parameters. The map is
the whole of what the account may see.

### Contents

Hexes the account can see are drawn with their terrain. Every other hex in the
window is drawn as fog: the hex outline, no terrain. A hex outside the world is
drawn as fog even when it is in the visible set, so the edge of the world is not
distinguishable from unexplored ground.

Coordinates are true axial `(q, r)` values, the same coordinates every other
account means. A fog hex carries no coordinate.

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

- The grid is pointy-top, even-r, matching the layout the world is generated in.
- One `polygon` element is drawn per hex. Its class is the terrain value, or
  `fog` when the hex is not visible.
- Each `polygon` carries a `title`. A visible hex gives its coordinate, its
  terrain and its elevation, where elevation is `N m` on land and `N m deep` in
  water. A fog hex gives `Unexplored` and nothing else.
- The element carries a `width` and a `height` in pixels as well as a `viewBox`,
  so it is drawn at its natural size rather than scaled to its container. The
  container scrolls.
- Hexes are drawn in a fixed order, so identical requests produce identical
  markup.

A map that straddles the meridian is drawn continuous across it. Each map says
which copy of a hex it wants: an admin window takes the copy nearest its own
centre column, and a player map the copy nearest the account origin.
