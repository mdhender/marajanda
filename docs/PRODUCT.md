# Product Reference

Marajanda is an open-ended fantasy game.

## Roles and factions

Accounts have exactly one of two mutually exclusive roles:

- `admin`: manages the server and game and controls the single Marajanda faction. Marajanda has almost god-like powers in the game.
- `player`: controls exactly one faction. A player faction starts with limited capabilities that increase through gameplay.

An account cannot have both roles or control multiple factions.

## Player faction configuration

A player must complete all required faction metadata before viewing the player dashboard. The initial required metadata is the faction name.

Faction names:

- contain 3 to 32 Unicode code points after normalization;
- use valid UTF-8;
- contain only printable code points and ASCII spaces;
- have leading and trailing spaces removed; and
- collapse each run of spaces to one space.

Accepted faction names do not need to be unique. Newly configured factions start at the axial coordinate `(0, 0)`.

## Map coordinates

The map uses a flat-top hex grid. Locations are displayed as axial `(q, r)` coordinates, with `(0, 0)` as the origin.
