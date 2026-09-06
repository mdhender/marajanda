// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"bytes"
	"embed"
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

// assetFiles holds the third-party files the pages load.
//
// The embed pattern names each file rather than globbing the directory, so
// assets/README.md and assets/LICENSE.htmx stay documentation and never become
// something the server will hand out.
//
//go:embed assets/htmx-2.0.10.min.js
var assetFiles embed.FS

// assetTypes is the set of servable assets and the type each is served as.
//
// A map rather than a file server: the route takes a name from the URL, and a
// lookup that can only ever succeed for a listed file is easier to be sure
// about than a path that is cleaned and hoped to stay inside the tree.
var assetTypes = map[string]string{
	htmxAsset: "text/javascript; charset=utf-8",
}

// asset serves one vendored file.
//
// The response is immutable because the version is in the file name: a browser
// that holds htmx-2.0.10.min.js holds the only thing that URL will ever mean,
// and an upgrade arrives as a different URL.
func (app *application) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	contentType, ok := assetTypes[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	content, err := assetFiles.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A zero modification time leaves Last-Modified off, which is what an
	// immutable response wants; ServeContent still answers range requests and
	// handles If-Range for it.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
}

// htmxPath is where the page asks for HTMX.
func htmxPath() string {
	return "/assets/" + htmxAsset
}
