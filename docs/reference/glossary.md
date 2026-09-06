# Glossary

## Origin

The game map's true cube coordinate `(0, 0, 0)`. Also called the **game
origin**. Unqualified uses of *origin* refer to the game origin.

## Polar ice

The `ice` terrain of the northernmost and southernmost rows of the world. Ice
is neither land nor water, and nothing may enter it. See
[Terrain reference](terrain.md).

## Origin hex

The hex a seated account starts on. It is a true map coordinate and is displayed
the same way to every account: there is no per-account coordinate system, and two
accounts naming `(12, -4)` mean one hex. An origin hex assigned by placement is
always land and is never the [game origin](#origin), which the main admin holds.
See [Player origin reference](player-origin.md).

## Compass point

One of the six directions a hex has a neighbour in: north-east, east,
south-east, south-west, west, north-west, in that order. The world is drawn
pointy-top, so there is no due north or due south neighbour. The order is a game
rule: rules that visit a hex's neighbours in turn use it. See
[Compass reference](compass.md).
