# Glossary

## Origin

The game map's true cube coordinate `(0, 0, 0)`. Also called the **game
origin**. Unqualified uses of *origin* refer to the game origin.

## Polar ice

The `ice` terrain of the northernmost and southernmost rows of the world. Ice
is neither land nor water, and nothing may enter it. See
[Terrain reference](terrain.md).

## Origin hex

The hex a faction's founding entities are placed on. It is the account's
permanent founding seat, not a current position: a faction has no location, and
its entities move.

It is a true map coordinate and is displayed the same way to every account:
there is no per-account coordinate system, and two accounts naming `(12, -4)`
mean one hex. An origin hex assigned by placement is always land and is never
the [game origin](#origin), which the main admin holds. See
[Player origin reference](player-origin.md).

## Faction

What a player controls. A faction has a name, a race, and no location of its
own: it owns [entities](#entity), and they are what stand on the map. See
[Product reference](../PRODUCT.md#roles-and-factions).

## Entity

Anything that stands in the world. An entity has a location, a
[code](#code), a name, and a [kind](#kind), and it belongs to one faction.
Orders are issued to entities. A leader is an entity; a settlement is an entity.
See [Entities reference](entities.md).

## Unit

Inventory: a quantity of a kind held by an entity, such as 40 archers or 300
gold. A unit has no code, no name and no identity of its own, so merging two
stacks is addition. See [Entities reference](entities.md).

## Leader

An [entity](#entity) of kind `leader`. A faction is founded with one, `LEADER-1`.

## Settlement

An [entity](#entity) that is a place rather than a person. `hamlet` is the only
settlement kind so far, and a faction is founded with one, `HAMLET-1`. It is an
entity rather than a [unit](#unit) because it has a location, a mutable kind, a
name a player may change, and inventory of its own.

## Code

An [entity](#entity)'s permanent label: its kind, then a per-faction sequence
for that kind. `LEADER-1`, `HAMLET-1`. A code is assigned at creation and frozen
there, so a hamlet that grows is still `HAMLET-1`. Codes are scoped to a
faction: two factions each have a `LEADER-1`.

## Name

An [entity](#entity)'s changeable label. It defaults to the entity's
[code](#code) and is changed through an order rather than a form.

## Kind

What an [entity](#entity) is: `leader` or `hamlet`. Kind is mutable and tracked
as a [fact](#fact), so growth from one kind to another is a new fact about one
entity rather than a new entity. An entity's kind decides which order kinds are
legal for it.

## Fact

A row that is true of one [entity](#entity) over a period of turns. Facts carry
`effective_from` and `effective_through` as turn numbers over the half-open
period `[from, through)`. See [effective dating](#effective-dating).

## Effective dating

Dating a [fact](#fact) in turns rather than overwriting it. A fact is true on a
turn when `effective_from <= turn AND turn < effective_through`. A period that
has not ended runs to the end-of-time turn, `99999999`, rather than to `NULL`.
The rows are the single source of truth: what was true on turn 7 is a query.
See [Entities reference](entities.md#effective-dating).

## Turn

The game's clock. An integer that starts at 1 and only ever increases, held once
per database. An order issued during a turn takes effect on the turn after it is
processed.

## Compass point

One of the six directions a hex has a neighbour in: north-east, east,
south-east, south-west, west, north-west, in that order. The world is drawn
pointy-top, so there is no due north or due south neighbour. The order is a game
rule: rules that visit a hex's neighbours in turn use it. See
[Compass reference](compass.md).
