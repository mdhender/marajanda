# Terrain reference

## Initialized hexes

An initialized hex records a true map coordinate and its terrain. Coordinates
are stored as axial `(q, r)` values; the cube component is `s = -q-r`.

The axial coordinate pair is the initialized hex's primary key. Terrain is
required and is one of:

- `grassland`
- `forest`
- `hills`
- `marsh`
- `mountains`

The game origin `(0, 0, 0)` is always initialized as mountains.

## Terrain stream

Every other hex uses a stream addressed by its true cube coordinate:

```text
[prng.TagHex, q, r, s]
```

The stream rolls one d10. The result determines terrain:

| Roll | Terrain |
| ---: | --- |
| 1–4 | `grassland` |
| 5–6 | `forest` |
| 7–8 | `hills` |
| 9 | `marsh` |
| 10 | `mountains` |

The same game seeds and true cube coordinate always produce the same terrain.
