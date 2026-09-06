// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// mapAdmin is the account the HTMX tests browse the admin map as.
var mapAdmin = datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}

func mapStore() applicationStore {
	return &testStore{game: testMapGame(), world: testMapWorld()}
}

func TestAssetRouteServesTheVendoredHTMX(t *testing.T) {
	handler := newHandler(nil, mapStore())
	request := httptest.NewRequest(http.MethodGet, "/assets/"+htmxAsset, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	// The asset is public: a browser has to be able to fetch the script before
	// it has a session, and the sign-in page loads it.
	if got := response.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("content type = %q, want text/javascript; charset=utf-8", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control = %q, want an immutable year", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q, want nosniff", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, "htmx") {
		t.Fatalf("the asset does not look like HTMX: %.60q", body)
	}
	if got, want := len(body), 40_000; got < want {
		t.Fatalf("asset is %d bytes, want at least %d - a truncated vendor file", got, want)
	}
}

// The route answers from a list of files rather than from the filesystem, so
// nothing outside that list is reachable through it - documentation in the
// asset directory included.
func TestAssetRouteServesNothingItWasNotGiven(t *testing.T) {
	handler := newHandler(nil, mapStore())
	for _, name := range []string{"README.md", "LICENSE.htmx", "htmx.min.js", "handler.go"} {
		request := httptest.NewRequest(http.MethodGet, "/assets/"+name, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("/assets/%s status = %d, want %d", name, response.Code, http.StatusNotFound)
		}
	}
}

func TestPagesLoadHTMXAndSaySoInThePolicy(t *testing.T) {
	handler := newHandler(nil, mapStore())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Body.String(), `<script src="/assets/`+htmxAsset+`" defer></script>`; !strings.Contains(got, want) {
		t.Fatalf("the page does not load HTMX from %s", "/assets/"+htmxAsset)
	}
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self'") {
		t.Fatalf("policy = %q, want script-src 'self'", policy)
	}
	// HTMX needs neither, and a policy that granted them would let the next
	// change go unnoticed.
	for _, forbidden := range []string{"unsafe-eval", "script-src 'unsafe-inline'", "https://"} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("policy = %q, want no %s", policy, forbidden)
		}
	}
}

func TestAdminMapAnswersHTMXWithTheRegionAlone(t *testing.T) {
	response := signedInMapHeaders(t, mapAdmin, mapStore(), "/admin/map?q=4&r=2",
		map[string]string{"HX-Request": "true"})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	// The region and nothing around it: no document, no heading, no legend,
	// and no sign-out form the browser already has.
	for _, unwanted := range []string{"<!doctype html>", "· Marajanda</title>", `class="legend"`, "/sign-out"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("fragment contains %q, want the map region alone", unwanted)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(body), `<div id="map-region"`) {
		t.Fatalf("fragment starts %.60q, want the map region element", strings.TrimSpace(body))
	}
	if got := strings.Count(body, "<polygon"); got == 0 {
		t.Fatalf("fragment draws %d polygons, want a map", got)
	}
	// It is the window that was asked for, not the origin.
	if want := coordLabel(hexg.NewHex(4, 2)); !strings.Contains(body, want) {
		t.Fatalf("fragment does not centre on %s", want)
	}
	// One URL with two shapes of answer has to say what it varied on.
	if got := response.Header().Get("Vary"); got != "HX-Request" {
		t.Fatalf("Vary = %q, want HX-Request", got)
	}
}

// HTMX asks for a history restore with HX-Request set, and puts the answer back
// as the whole body. Answering that with a fragment breaks the back button.
func TestAdminMapAnswersAHistoryRestoreWithThePage(t *testing.T) {
	response := signedInMapHeaders(t, mapAdmin, mapStore(), "/admin/map",
		map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.HasPrefix(body, "<!doctype html>") {
		t.Fatalf("restore starts %.60q, want a whole page", body)
	}
}

func TestAdminMapPageWiresPanningToHTMX(t *testing.T) {
	response := signedInMapHeaders(t, mapAdmin, mapStore(), "/admin/map", nil)
	body := response.Body.String()

	if !strings.Contains(body, `<div id="map-region" hx-target="#map-region" hx-swap="outerHTML" hx-push-url="true"`) {
		t.Fatalf("the page has no swappable map region")
	}
	// Progressive enhancement is the point: every control HTMX drives is still
	// the link or form that worked without it.
	for _, want := range []string{
		fmt.Sprintf(`href="/admin/map?q=%d&amp;r=0" hx-get="/admin/map?q=%d&amp;r=0"`, adminWindowColumns/2, adminWindowColumns/2),
		`<form class="map-jump" action="/admin/map" method="get" hx-get="/admin/map">`,
		`href="/admin/map" hx-get="/admin/map"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the page is missing %s", want)
		}
	}
}

// A player's map has no controls, so it has no HTMX on it - but it is drawn by
// the same block, and the block has to render for a page that has no pan.
func TestPlayerMapRendersTheRegionWithoutControls(t *testing.T) {
	account := datastore.Account{Email: "player@example.com", Handle: "scout", Role: "player", Origin: playerOrigin}
	store := &testStore{
		game:    testMapGame(),
		world:   testMapWorld(),
		faction: datastore.Faction{Name: "The Hearth", Race: game.RaceHuman, Active: true},
		found:   true,
		visible: []hexg.Hex{playerOrigin},
	}
	response := signedInMap(t, account, store, "/player/map")

	body := response.Body.String()
	if !strings.Contains(body, `<div id="map-region"`) {
		t.Fatalf("the player map has no map region")
	}
	for _, unwanted := range []string{`class="map-pan"`, `class="map-jump"`, "hx-get"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the player map contains %q, and a player has nothing to pan to", unwanted)
		}
	}
}
