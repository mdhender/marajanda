# About wilderness travel costs borrowed from d20 games

Marajanda charges action points for movement, and so do the wilderness travel
rules of the tabletop games it descends from. Those rules have been played for
decades, which makes them a tempting place to shop: terrain costs, mounts,
encumbrance, forage, weather and getting lost are all problems somebody has
already balanced.

This document collects one such body of ideas and works out how far it is from
the rules this game actually has. **It is informative, not normative.** Nothing
here has been decided, nothing in the code implements it, and no other document
is bound by it. It is kept because the ideas are worth having in front of us
when the placeholder movement cost is replaced.

The rules that do bind are
[docs/reference/action-points.md](../reference/action-points.md), which owns the
allowance and what a step costs,
[docs/reference/turn-processing.md](../reference/turn-processing.md), which owns
what happens when those costs are charged, and
[docs/reference/terrain.md](../reference/terrain.md), which owns what the world
is made of.

## Where the material came from

It is a conversation with a chat assistant about wilderness travel in a d20
game, transcribed. It was written for a table with a referee, a party of
characters and a map measured in miles, and it read that way: it addressed a
game master in the second person and offered to design the next mechanic.

The prose has been rewritten. The numbers below are still the source's numbers
on the source's assumptions, and none of them has been converted to anything.

## The assumptions it rests on, and ours

The gap is wider than a table of costs. The source and this game disagree about
what an action point is, how many there are, and what the map is made of.

| The source assumes | Marajanda has |
| --- | --- |
| 1 AP is one day of travel | An abstract unit. Nothing says how long a turn is. |
| 32 AP in a turn | A leader's allowance is **6**. `MaxOrdersPerEntity` is 32, and that is a storage bound on how many orders may be written, not an allowance. |
| Fractional AP: 0.5, 1.5, +0.5 | Integer costs. An order costs a whole number of points. |
| Six-mile hexes | A dimensionless map. Nothing in the generator or the datastore knows how far a hex is; see [world size](world-size.md). |
| Plains, roads, jungle, desert, swamp | `grassland`, `forest`, `hills`, `marsh`, `mountains`, `ocean`, `lake`, and `ice`. Water and ice stop ordinary entities. No roads, no rivers, no desert, no jungle. |
| A party of characters with classes | Entities: a `leader`, a `hamlet`, and Marajanda. No characters, no classes, no spells. |
| A referee rolling dice at the table | Deterministic processing. Randomness is addressed through `seeds.Roller`, never rolled. |
| Cost varies with the terrain entered | Cost varies with **whether the faction knew the hex**: 1 AP known, 3 AP unknown, flat over terrain. |

The last row is the one that matters most, and the next section is about it.

## Why cost does not vary with terrain today

The obvious criticism of the current rule is that a step into mountains costs
what a step into grassland costs, which is plainly wrong as simulation. The
reference document says the flat pair is a placeholder and that
[#37](https://github.com/mdhender/marajanda/issues/37) replaces it. It is worth
being clear about what makes that replacement harder than substituting a table.

A player's orders are priced **before** the turn is processed, on the orders
page, from what the faction knows, and that price is a number the player reads.
So a cost that varied with the terrain entered would announce the terrain of a
hex nobody has been to: order a step into the fog, read `3 AP`, and the fog has
told you it is not mountains. The flat exploration price exists to say nothing.

That is not an argument against a terrain-varying cost. It is a constraint on
one: the price of a step into unknown ground has to be the same whatever is
there, so a terrain rule can only price ground the faction has already seen. The
source's matrices assume a referee who can see the whole map and a party that
finds out what a hex costs by walking into it. That is a different information
model, not merely a different table.

Two of its ideas survive the constraint intact, because they price what the
faction knows about itself rather than about the ground: what an entity is
carrying, and what it is travelling with.

## The four axes it proposes

### Terrain

| Terrain | Pass through | Search thoroughly |
| --- | ---: | ---: |
| Easy: plains, grassland, roads | 1 AP | 2 AP |
| Normal: hills, light forest | 1 AP | 3 AP |
| Difficult: jungle, mountains, swamp | 2–3 AP | 4+ AP |

Three bands rather than a cost per terrain, which is a reasonable shape for us:
five land terrains collapse into easy, normal and difficult without much
argument, and `marsh` and `mountains` are obviously the difficult ones.

The second column is a separate idea, and worth noticing on its own. **Passing
through a hex and searching it are different acts at different prices.** That
distinction is already in the knowledge record — `observed` against `explored` —
but not as something a player may choose to pay for. Nothing lets an entity
spend points to learn more about where it stands.

### Mounts and vehicles

| | Easy | Normal | Difficult |
| --- | ---: | ---: | ---: |
| On foot | 1 AP | 1 AP | 2–3 AP |
| Riding horses | 0.5 AP | 1 AP | Impassable |
| Wagons and carts | 1 AP | 2 AP | Impassable |

The trade is the point: horses are faster only where the ground is open, wagons
are never faster and buy capacity instead, and both turn difficult terrain from
expensive into impossible.

Adopting any of it needs something that says what an entity travels with.
`units` — the inventory table from
[#32](https://github.com/mdhender/marajanda/issues/32) — is where that would
live, and nothing produces a unit yet. Passability already depends on the mover
as well as the hex: water and ice stop leaders and hamlets, while terrain does
not stop Marajanda. Inventory granting another exception remains unimplemented.

### Supply: encumbrance, forage and water

The source's longest thread, and the one that argues with itself. Pack animals
carry loot, animals eat, feed is heavier than the loot it displaces, and a mule
on a thirty-two day expedition cannot carry its own food. Grazing is the
alternative, paid for in AP rather than in weight, and unavailable in the
terrains where nothing grows.

As a design it is a closed loop, and an appealing one. As something to build it
is the furthest away: it wants inventory, weights, capacities, a supply state
that survives a turn boundary, and terrain that knows whether anything grows
there. Its arithmetic assumes miles and days, so none of that transfers — only
the shape does.

Its most portable idea is the smallest one: **carrying more should cost time,
not only space.** That is a rule we could hold with no animals in the game at
all.

### Weather and getting lost

| Weather | Modifier |
| --- | ---: |
| Light rain or snow | none |
| Heavy rain | +0.5 AP, and more for wheels |
| Severe storm or blizzard | +1 to +2 AP, or shelter for a turn |
| Extreme heat | +1 AP, or double the water |

Weather here is a multiplier on the map that changes from turn to turn, plus a
navigation check that wastes points when it fails.

Both halves land on the determinism contract, and it is worth being precise
about how. Neither is forbidden. A keyed roller is exactly the tool for "what is
the weather on turn 7 over this region", and it would produce the same weather
on every replay. What is forbidden is a roll made in the moment from state
nothing records: the source's secret d6 for a bad tavern rumour has no address,
so no replay could reproduce it. Anything adopted here has to be derived from
the seeds and a key made of recorded values.

The world already has weather in one sense and not in the other. Generation uses
moisture and rain shadow to decide what a hex *is*, once, and stores the result;
nothing models weather passing over the map afterwards, and there is nowhere to
put it.

The navigation half assumes something we deliberately do not have: a party that
can end up somewhere other than where it steered. A move order names a compass
point, and the entity either enters that hex or stays where it was. A step that
lands somewhere the player did not name is a much larger change to what an order
means than a cost modifier is.

## What it would take to use any of it

In rough order of how much has to exist first:

1. **Terrain bands.** Needs only the cost rule and a decision about what unknown
   ground costs, which [#37](https://github.com/mdhender/marajanda/issues/37)
   already owns.
2. **Race as a modifier.** Also #37. The source says nothing about it — it has
   no races. Ours does.
3. **Searching a hex as a paid act.** Needs an order kind, and something for
   `explored` to mean beyond what it means now.
4. **Encumbrance.** Needs `units` to hold something.
5. **Mounts and vehicles.** Needs units, and impassability that depends on the
   mover.
6. **Supply and forage.** Needs all of the above, plus state that survives a
   turn boundary.
7. **Weather.** Needs a weather model, a domain tag, and a decision about
   whether a player can see a forecast.
8. **Getting lost.** Needs a move order that can fail into the wrong hex.

## What it leaves open

**What an action point is.** The source's answer — one AP is one day — is the
most useful thing in it, because it turns every cost question into a question
about how long something takes. We have not answered it. Six points a turn with
no stated duration means a turn is however long six actions take, which is a way
of not needing the answer yet rather than an answer.

**Whether the allowance stays at six.** Every table above was balanced against
thirty-two. A `+1 AP` modifier is a rounding error in a budget of thirty-two and
a sixth of the turn in a budget of six, so nothing here transfers without
rescaling — and the rescaling is not a division, because fractional costs do not
exist for us and `+1` is the smallest modifier we can express.

**Whether a cost should stay exactly legible.** Today every order is priced
exactly, on the page, before the turn runs. The more inputs a cost has — weather,
supply, what an entity carries — the harder that is to keep, and at some point
the honest answer is an estimate allowed to be wrong. That would change what the
orders page promises. See
[the two engines](../reference/action-points.md#the-two-engines).

## Sources

The transport, forage and water figures carried the assistant's own citations:

- [How much water does a horse drink per day](https://systemequine.com/how-much-water-does-a-horse-drink-per-day/)
- [House rules for extreme weather](https://www.enworld.org/threads/my-house-rules-for-extreme-weather-and-sleeping-in-armor.524874/)
- [Water requirements for horses](https://veterinarypartner.vin.com/default.aspx?pid=19239&catId=254019&id=4952327)
