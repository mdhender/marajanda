# Assets

Files served by the `GET /assets/{name}` route in [`assets.go`](../assets.go).
Third-party ones are checked in rather than fetched at build time or loaded from
a CDN, because `render` sets `default-src 'self'` and the page cannot reach
another origin.

| File | Version | Source | Licence | SHA-256 of the vendored file |
| --- | --- | --- | --- | --- |
| `htmx-2.0.10.min.js` | htmx 2.0.10 | `https://unpkg.com/htmx.org@2.0.10/dist/htmx.min.js` | 0BSD (`LICENSE.htmx`) | `71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de` |

The bytes were taken from unpkg and compared against
`https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js`; the two CDNs
agree on the digest above.

## Upgrading

1. Download the new `htmx.min.js` from both CDNs and confirm the digests match.
2. Add it here as `htmx-<version>.min.js` and delete the old file.
3. Update `htmxAsset` in [`assets.go`](../assets.go) and the row above.

The version is part of the file name because the route serves these
`immutable`: a new version is a new URL, so no browser has to be talked out of
a cached copy of the old one.

## First-party

| File | What it is for |
| --- | --- |
| `marajanda.js` | Turning the orders form off when a write loses a race |

`marajanda.js` is the whole of the project's own script and is meant to stay
that way: HTMX drives the interactions, and this is for the one thing HTMX has
no way to reach. A conflicting write is answered with the notice alone, so the
response is not drawing the controls and cannot put `disabled` on them; CSS
cannot stand in, because `pointer-events` stops a mouse and leaves a keyboard
and a screen reader with a live form. See
[#66](https://github.com/mdhender/marajanda/issues/66).

Its name carries no version, because it changes with the binary. The route
serves it `no-cache` with an ETag of its own bytes instead, so a browser asks on
every load and is answered `304` until the file actually changes.
