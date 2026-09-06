# Product Reference

Marajanda is an open-ended fantasy game.

## Roles and factions

Accounts have exactly one of two mutually exclusive roles:

- `admin`: manages the server and game and controls the single Marajanda faction. Marajanda has almost god-like powers in the game.
- `player`: controls exactly one faction. A player faction starts with limited capabilities that increase through gameplay.

An account cannot have both roles or control multiple factions.

## Player faction configuration

A player must complete all required faction metadata before viewing the player dashboard. The required metadata is the faction name and the faction's race.

Faction names:

- contain 3 to 32 Unicode code points after normalization;
- use valid UTF-8;
- contain only printable code points and ASCII spaces;
- have leading and trailing spaces removed; and
- collapse each run of spaces to one space.

Accepted faction names do not need to be unique. Newly configured factions start at the axial coordinate `(0, 0)`.

A faction's race is one of `human`, `elf`, `dwarf`, `orc`, `kobold`, or `halfling`. It defaults to `human` when none is chosen, and a race outside that list is rejected. Race decides only where a faction is settled; it has no other effect on play. See [Terrain reference](reference/terrain.md#race-terrain-preference) for each race's terrain preference.

Configuring a faction is what gives a player account its origin hex, because placement depends on the race. A player account has no origin between its creation and that moment.

## Map coordinates

The map uses a pointy-top hex grid in even-r layout. Locations are displayed as true axial `(q, r)` coordinates, with `(0, 0)` as the game origin. Every account sees the same coordinate for the same hex.

## The world

A game has one world, bounded and generated once from the game seeds when the game is created. It holds land, ocean, and lakes, and every hex has an elevation. The world wraps east-west and is walled north and south by impassable sheets of polar ice. See [Terrain reference](reference/terrain.md).

## Player origin

A faction starts on land inside the equatorial belt, which reaches two thirds of the way from the equator to each pole. Nobody is settled in the cold margins against the polar ice.

Within the belt, a faction is settled on the terrain its race most favours that still has room for it. Two factions of the same race on the same terrain are kept at least 13 hexes apart; every other pair is kept at least 8 hexes apart. See [Player origin reference](reference/player-origin.md).
