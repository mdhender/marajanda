# Vendored assets

Third-party files served by the `GET /assets/{name}` route in
[`assets.go`](../assets.go). They are checked in rather than fetched at build
time or loaded from a CDN, because `render` sets `default-src 'self'` and the
page cannot reach another origin.

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
