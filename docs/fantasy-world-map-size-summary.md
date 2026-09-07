# Fantasy World Map Size Summary

## Project Summary

The world generator should produce a rectangular hex map whose **land area is approximately equal to the total land area of Earth**.

The working assumptions are:

- Earth land area: **~57.5 million square miles**
- Generated terrain: **58% land, 42% water**
- Hex shape: regular hexagon
- Hex apothem: **6 miles**
- Target map aspect ratio: **2.39:1** (width:height)

Because only 58% of the generated map is expected to be land, the complete map must be larger than Earth's land area alone.

The resulting target is approximately:

- **795,000 total hexes**
- **1,379 hexes wide**
- **577 hexes high**
- **795,683 actual hex positions**

At the expected 58% / 42% terrain split, this produces approximately:

- **461,496 land hexes**
- **334,187 water hexes**

The land portion represents approximately **57.55 million square miles**, which is very close to Earth's actual land area.

## Recommended Generator Target

Use:

> **1379 × 577 hexes**

as the nominal world-map dimensions.

This provides:

- an aspect ratio of approximately **2.38995:1**
- approximately **795,683 total hexes**
- enough total surface area for 58% of the generated cells to represent roughly Earth's land area
- a simple fixed target suitable for terrain generation, testing, storage estimates, and performance planning

The generator should continue to determine land and water from the elevation histogram rather than forcing an exact hex count. Converting the lowest **42% of elevation buckets** to water should yield approximately the desired 58% land coverage over a sufficiently varied terrain distribution.

Small deviations in the exact land/water count are expected and should not be treated as errors.

---

# Appendix A — Math for Coding Agents

## 1. Area of One Hex

For a regular hexagon with apothem `a`:

```text
area = 2 * sqrt(3) * a^2
```

With:

```text
a = 6 miles
```

the area is:

```text
area = 2 * sqrt(3) * 6^2
     = 72 * sqrt(3)
     ≈ 124.707658 square miles
```

So each hex represents approximately:

```text
124.71 square miles
```

## 2. Required Total Map Area

Target land area:

```text
landArea = 57,500,000 square miles
```

Expected land fraction:

```text
landFraction = 0.58
```

Therefore:

```text
totalArea = landArea / landFraction
          = 57,500,000 / 0.58
          ≈ 99,137,931 square miles
```

## 3. Required Total Hex Count

```text
hexCount = totalArea / hexArea
         = 99,137,931 / 124.707658
         ≈ 794,963 hexes
```

For planning purposes:

```text
targetHexCount ≈ 795,000
```

## 4. Deriving Width and Height

Desired aspect ratio:

```text
width / height = 2.39
```

Let:

```text
width = 2.39 * height
```

and:

```text
width * height ≈ 794,963
```

Then:

```text
2.39 * height^2 ≈ 794,963
```

so:

```text
height ≈ sqrt(794,963 / 2.39)
       ≈ 576.7
```

and:

```text
width ≈ 2.39 * 576.7
      ≈ 1,378.4
```

A convenient integer choice is:

```text
width  = 1379
height = 577
```

## 5. Resulting Map Size

```text
1379 * 577 = 795,683 hexes
```

Actual aspect ratio:

```text
1379 / 577 ≈ 2.38995
```

This is effectively the desired:

```text
2.39 : 1
```

## 6. Expected Land and Water Counts

For 58% land:

```text
landHexes = 795,683 * 0.58
          ≈ 461,496
```

For 42% water:

```text
waterHexes = 795,683 * 0.42
           ≈ 334,187
```

Check:

```text
461,496 + 334,187 = 795,683
```

## 7. Approximate Land Area Produced

```text
landArea = landHexes * hexArea
         ≈ 461,496 * 124.707658
         ≈ 57.55 million square miles
```

This is close enough to the target Earth land area of approximately **57.5 million square miles** that no further dimensional adjustment is necessary.

## 8. Suggested Constants

Illustrative pseudocode:

```text
HexApothemMiles = 6.0

MapWidth  = 1379
MapHeight = 577

WaterFraction = 0.42
LandFraction  = 0.58
```

If elevation values are converted to histogram buckets, the terrain classifier should treat approximately the lowest 42% of the elevation distribution as water.

The precise implementation may use percentile thresholds rather than assuming that exactly 42 histogram bucket indices correspond to 42% of generated cells. If the histogram buckets contain unequal populations, percentile-by-cell-count will produce a more reliable land/water ratio than percentile-by-bucket-number.

# Appendix B — References

* [An overview of 6 mile hexes](https://youtu.be/IBvuV9zuXnQ)
