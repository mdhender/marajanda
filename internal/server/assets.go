// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"time"
)

// htmxAsset is the vendored HTMX build, named with its version.
//
// HTMX is served from the binary rather than from a CDN because render sets
// "default-src 'self'": a CDN script is blocked outright, and widening the
// policy to trust another origin costs more than carrying fifty kilobytes.
// Vendoring also keeps the server one artifact with no network dependency at
// boot, which is what the rest of the stack already assumes.
//
// See assets/README.md for provenance and the upgrade steps.
const htmxAsset = "htmx-2.0.10.min.js"

// pageAsset is the project's own script: the one job on the pages that HTML and
// CSS cannot do, which is turning the orders form off when a write loses a race
// and the response is not drawing the form. See assets/marajanda.js and #66.
//
// It is deliberately the whole of the first-party script. HTMX is what drives
// the interactions; this is for what HTMX has no way to reach.
const pageAsset = "marajanda.js"

// assetFiles holds the files the pages load.
//
// The embed pattern names each file rather than globbing the directory, so
// assets/README.md and assets/LICENSE.htmx stay documentation and never become
// something the server will hand out.
//
//go:embed assets/htmx-2.0.10.min.js assets/marajanda.js
var assetFiles embed.FS

// servedAsset is one file the route will hand out: what it is served as, and
// how long a browser may keep it.
type servedAsset struct {
	contentType string
	// immutable says the file's name carries its identity, so a browser that
	// has it holds the only thing that URL will ever mean. It is true of a
	// vendored file named with its version and false of ours, which changes
	// with the binary under a name that does not.
	immutable bool
}

// assetTypes is the set of servable assets.
//
// A map rather than a file server: the route takes a name from the URL, and a
// lookup that can only ever succeed for a listed file is easier to be sure
// about than a path that is cleaned and hoped to stay inside the tree.
var assetTypes = map[string]servedAsset{
	htmxAsset: {contentType: "text/javascript; charset=utf-8", immutable: true},
	pageAsset: {contentType: "text/javascript; charset=utf-8"},
}

// asset serves one embedded file.
//
// A version-named file is served immutable: an upgrade arrives as a different
// URL, so no browser has to be talked out of a cached copy of the old one. Ours
// is not version-named, so it is served with a tag of its own contents instead -
// a browser asks on every load and is answered 304 until the file actually
// changes, which is what keeps a deployed fix from waiting behind a cache.
func (app *application) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	served, ok := assetTypes[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	content, err := assetFiles.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", served.contentType)
	if served.immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", assetETag(content))
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A zero modification time leaves Last-Modified off, which is what an
	// immutable response wants; ServeContent still answers range requests and
	// handles If-Range for it, and honours the ETag above when there is one.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
}

// assetETag tags a file by its contents, so two builds that did not change it
// answer with the same tag and a browser keeps what it has.
func assetETag(content []byte) string {
	digest := sha256.Sum256(content)
	return `"` + hex.EncodeToString(digest[:16]) + `"`
}

// pageScripts are the scripts every page loads, in the order they load.
//
// HTMX is first because ours listens for its events; both are deferred, so the
// order here is the order they run rather than a race.
func pageScripts() []string {
	return []string{"/assets/" + htmxAsset, "/assets/" + pageAsset}
}
