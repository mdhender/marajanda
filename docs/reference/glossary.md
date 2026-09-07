# Glossary

## Action point

The unit an [entity](#entity) spends to act, abbreviated AP.
A step onto ground the faction knew when the turn opened costs 1; a step onto ground it did not costs 3, whatever the terrain.
See [Action points reference](action-points.md).

## Active

Whether a thing may act.
An account and a [faction](#faction) each carry the flag, and the two are independent.
A deactivated account cannot sign in; a deactivated faction cannot give [orders](#order), and its player can still sign in and look at their game.
Everything is created active, and the flag is set by hand during beta.
See [Accounts reference](../ACCOUNTS.md#deactivation).

## Allowance

How many [action points](#action-point) an [entity](#entity) has for one [turn](#turn).
A newly created leader's is 6.
It is a [fact](#fact) of the entity and not a running balance: nothing carries into the next turn, and processing never writes a total back.
An entity whose [kind](#kind) accepts no [orders](#order) has no allowance, and having none is the absence of a row rather than a zero in one.
See [Action points reference](action-points.md#the-allowance).

## Code

An [entity](#entity)'s permanent label: its kind, then a per-faction sequence for that kind.
`LEADER-1`, `HAMLET-1`.
A code is assigned at creation and frozen there, so a hamlet that grows is still `HAMLET-1`.
Codes are scoped to a faction: two factions each have a `LEADER-1`.

## Compass point

One of the six directions a hex has a neighbour in: north-east, east, south-east, south-west, west, north-west, in that order.
The world is drawn pointy-top, so there is no due north or due south neighbour.
The order is a game rule: rules that visit a hex's neighbours in turn use it.
See [Compass reference](compass.md).

## Effective dating

Dating a [fact](#fact) in turns rather than overwriting it.
A fact is true on a turn when `effective_from <= turn AND turn < effective_through`.
A period that has not ended runs to the end-of-time turn, `99,999,999`, rather than to `NULL`.
The rows are the single source of truth: what was true on turn 7 is a query.
See [Entities reference](entities.md#effective-dating).

## Entity

Anything that stands in the world.
An entity has a location, a [code](#code), a name, and a [kind](#kind), and it belongs to one faction.
Orders are issued to entities.
A leader is an entity; a settlement is an entity.
See [Entities reference](entities.md).

## Estimate

What the [pre-processor](#pre-processor) answers with, and the word the orders page uses.
Every order is priced as though it lands, and unknown ground at a flat cost whatever is actually there.
The [executor](#executor) charges the real cost; the difference is the risk of exploration.
See [Action points reference](action-points.md#the-pre-processors-numbers-are-an-estimate).

## Executor

What walks an [entity](#entity)'s [orders](#order) when the [turn](#turn) is processed and charges them.
It writes no orders and it decides what happened.
Its twin is the [pre-processor](#pre-processor).
See [Action points reference](action-points.md#the-two-engines) and [Turn processing reference](turn-processing.md).

## Exhaust

The failure of a step an [entity](#entity) cannot afford.
An entity is walked until its [allowance](#allowance) will not pay for its next step; the steps it took stand, it stops where it stopped, and every remaining step is recorded as exhausted.
Nothing is rejected at order entry for being too long.
See [Action points reference](action-points.md#exhaust).

## Explored

The stronger of the two things a faction may know about a hex: its terrain, and what was standing in it when the faction's [entity](#entity) was there.
An entity entering a hex explores it.
Explored outranks [observed](#observed) and nothing goes backwards, so a later sighting from next door does not write an explored hex back down.
See [Knowledge reference](knowledge.md).

## Fact

A row that is true of one [entity](#entity) over a period of turns.
Facts carry `effective_from` and `effective_through` as turn numbers over the half-open period `[from, through)`.
See [effective dating](#effective-dating).

## Faction

What an account may control. A player controls one player faction; the seeded
main admin controls the single Marajanda faction.
A faction has a name, a race, and no location of its own: it owns [entities](#entity), and they are what stand on the map.
See [Product reference](../PRODUCT.md#roles-and-factions).

## Kind

What an [entity](#entity) is: `leader`, `hamlet`, or `marajanda`.
Kind is mutable and tracked as a [fact](#fact), so growth from one kind to another is a new fact about one entity rather than a new entity.
An entity's kind decides which order kinds are legal for it.

## Leader

An [entity](#entity) of kind `leader`.
A faction is founded with one, `LEADER-1`.

## Name

An [entity](#entity)'s changeable label.
It defaults to the entity's [code](#code) and is changed through an order rather than a form.

## Observed

The weaker of the two things a faction may know about a hex: its terrain type, its elevation, and whether it can be entered.
Nothing about what stands in it.
An [entity](#entity) entering a hex observes the six hexes around it.
A hex the faction has neither observed nor [explored](#explored) is unknown, which is the absence of a record rather than a third state.
See [Knowledge reference](knowledge.md).

## Observation

One hex one [order](#order) revealed, and the state it was revealed in.
A step that lands produces up to seven: the hex it entered [explored](#explored), the six around it [observed](#observed).
It is the grain a [result](#result) records sightings on; what the faction ends the turn knowing collapses every entity's sightings into one state per hex.
See [Turn results reference](turn-results.md#observations).

## Order

One instruction issued to one [entity](#entity) for one [turn](#turn), also called a stanza.
An order is one action: `move` walks the entity one hex in the [compass point](#compass-point) it names, and [rest](#rest) spends action points and moves nothing.
An entity's [kind](#kind) decides which order kinds it accepts: a leader accepts both, while a hamlet and Marajanda accept nothing.
Only the current turn's orders are writable; advancing the turn freezes the turn before it.
See [Orders reference](orders.md).

## Origin

The game map's true cube coordinate `(0, 0, 0)`.
Also called the **game origin**. Unqualified uses of *origin* refer to the game origin.

## Origin hex

The hex a faction's founding entities are placed on.
It is the account's permanent founding seat, not a current position: a faction has no location, and its entities move.

It is a true map coordinate and is displayed the same way to every account: there is no per-account coordinate system, and two accounts naming `(12, -4)` mean one hex.
An origin hex assigned by placement is always land and is never the [game origin](#origin), which the main admin holds.
See [Player origin reference](player-origin.md).

## Polar ice

The `ice` terrain of the northernmost and southernmost rows of the world.
Ice is neither land nor water. It stops leaders and hamlets, while Marajanda
may enter it; no entity may step beyond the world.
See [Terrain reference](terrain.md).

## Pre-processor

What prices an [entity](#entity)'s [orders](#order) during order entry and keeps its [trailing Rest](#trailing-rest).
It writes orders on the player's behalf and binds nothing: its numbers are an [estimate](#estimate) rather than a quote.
Its twin is the [executor](#executor).
See [Action points reference](action-points.md#the-two-engines).

## Rest

An [order](#order) kind that spends [action points](#action-point) and moves nothing.
A leader accepts it, it carries a count rather than a direction, it costs 1 AP per point, and it may be ordered more than once and anywhere in an entity's list.
A rest at the end of a list is the [trailing Rest](#trailing-rest).
What it recovers is not yet decided.
See [Action points reference](action-points.md#rest).

## Result

The record of what one [entity](#entity)'s [turn](#turn) was: its action point ledger, one outcome per [order](#order), and one [observation](#observation) per hex an order revealed.
Orders are what a player asked for and results are what the engine decided, so they are separate records and nothing writes an outcome onto an order row.
Every entity of an active [faction](#faction) has one for every turn it was processed on, whether or not it was given anything to do.
See [Turn results reference](turn-results.md).

## Settlement

An [entity](#entity) that is a place rather than a person.
`hamlet` is the only settlement kind so far, and a faction is founded with one, `HAMLET-1`.
It is an entity rather than a [unit](#unit) because it has a location, a mutable kind, a name a player may change, and inventory of its own.

## Step

One hex of movement in a turn's results: a `move` [order](#order) that is carried out is a step taken.
It is not a unit of order entry. An order is one action, so a step is what a move produces rather than something an order carries a list of.
See [Orders reference](orders.md).

## Trailing Rest

The [rest](#rest) an [entity](#entity)'s [order](#order) list ends with, whose count is what the orders before it leave unspent.
The line is rendered always and the row is stored only when the count is at least one, so the page keeps its shape and nothing stores a `Rest x0`.
See [Action points reference](action-points.md#the-trailing-rest).

## Turn

The game's clock.
An integer that starts at 1 and only ever increases, held once per database.
An [order](#order) issued during a turn takes effect on the turn after it is processed.
The admin advances the turn, which closes the orders built for it.

## Turn processing

What the admin's advance does before it moves the clock: it carries out the [orders](#order) of the turn being closed and writes what they produced.
Every [fact](#fact) it writes is effective from the turn after the one it processed, and the orders themselves are left exactly as they were written.
A deactivated [faction](#faction) is not processed.
See [Turn processing reference](turn-processing.md).

## Unit

Inventory: a quantity of a kind held by an entity, such as 40 archers or 300 gold.
A unit has no code, no name and no identity of its own, so merging two stacks is addition.
See [Entities reference](entities.md).
