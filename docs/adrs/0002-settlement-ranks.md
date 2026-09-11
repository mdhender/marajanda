# ADR 0002: Model hamlets as ranked settlements

- Status: Accepted
- Date: 2026-09-11

## Context

Marajanda currently calls a faction's founding settlement a `hamlet`. That name
describes its initial scale, but it does not describe what the entity remains as
it grows in population, stability, and importance. Treating each later rank as
a different kind of entity would confuse a change in scale with a change in
identity.

The ranks also carry an intentional shift in meaning:

- **Hamlet through City** describe a population centre becoming larger.
- **Barony through Kingdom** describe that place becoming the centre of an
  increasingly important surrounding realm.

At the second transition, the population centre has not become a government.
Its importance has crossed the point where people conventionally describe it
in terms of the territory administered from it. The higher-rank titles
therefore abstract both the population centre and the geographic area organized
around it. They do not make precise claims about government, sovereignty, or
law.

Players may give settlements names that do not follow these conventions. The
game still needs consistent default language for presenting settlement scale.

## Decision

The entity is a **Settlement**. Its scale and importance are represented by an
integer `level` from 1 through 9, inclusive. A settlement remains the same
entity when its level changes.

Each level has a conventional suggested title:

| Level | Suggested title | Narrative meaning |
| ---: | --- | --- |
| 1 | Hamlet | A small population centre |
| 2 | Village | A growing population centre |
| 3 | Town | A substantial population centre |
| 4 | City | A major population centre |
| 5 | Barony | The centre of a small surrounding realm |
| 6 | County | The centre of a larger surrounding realm |
| 7 | Duchy | The centre of a powerful surrounding realm |
| 8 | Grand Duchy | The centre of a pre-eminent surrounding realm |
| 9 | Kingdom | The centre of the greatest surrounding realm |

The mapping is equivalent to:

```text
SuggestedTitle(1) = "Hamlet"
SuggestedTitle(2) = "Village"
SuggestedTitle(3) = "Town"
SuggestedTitle(4) = "City"
SuggestedTitle(5) = "Barony"
SuggestedTitle(6) = "County"
SuggestedTitle(7) = "Duchy"
SuggestedTitle(8) = "Grand Duchy"
SuggestedTitle(9) = "Kingdom"
```

The title is descriptive flavor, not the settlement's identity or a legal
classification. In particular, level 6 means approximately “the scale of
settlement for which County is the conventional title”; it does not assert that
the faction has a count, a particular system of government, or legally defined
county borders.

A settlement's player-chosen name is independent of its suggested title. User
interfaces should suggest the conventional title for the current level to keep
default language consistent, without requiring players to incorporate that
title into the settlement's name.

The rules that calculate a settlement's level from population and stability
are not decided here. Neither are the rules for gaining or losing levels.

## Consequences

- `hamlet` becomes the conventional title of a level-1 Settlement rather than
  the enduring kind of the entity.
- Growth changes `Settlement.level`; it does not replace the Settlement with a
  Village, Town, or realm entity.
- Game rules should depend on settlement identity and level, not parse or infer
  meaning from the suggested title or player-chosen name.
- Presentation can use one stable level-to-title mapping while allowing
  unrestricted proper names.
- Features that need political institutions, offices, borders, or sovereignty
  must model them separately instead of inferring them from a settlement's
  level.
- Migrating the current `hamlet` kind, existing permanent `HAMLET-*` codes,
  datastore representation, and API fields requires a separate implementation
  decision that preserves existing identity guarantees.

## Alternatives considered

### Make every rank an entity kind

This would represent growth by changing a Settlement from `hamlet` to `village`
and onward. It makes rank names mechanically authoritative and obscures that
the entity persists while its scale changes.

### Model the higher ranks as governments

This would make City-to-Barony growth change the nature of the entity. It does
not fit the intended narrative: the population centre remains a settlement,
while the title increasingly describes the realm organized around it.

### Store only the title

A free-form title cannot provide a stable value for game rules or progression.
The numeric level is the abstraction; the suggested title is its conventional
presentation.
