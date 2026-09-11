// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
)

// Every orders representation carries a tag, and the write responses carry the
// tag of the list they just made, so a client never has to re-read to write
// twice.
func TestAPIOrdersRespondWithATag(t *testing.T) {
	for _, target := range []string{"/api/v1/orders", "/api/v1/turns/2/orders"} {
		t.Run(target, func(t *testing.T) {
			response := apiRead(t, apiReadStore(), "player", target)
			response.requireOK(t)
			tag := response.Header().Get("ETag")
			if _, ok := parseAPIETag(tag); !ok {
				t.Fatalf("ETag = %q, want one quoted entity-tag", tag)
			}
		})
	}
}

// A read that fails to tag still answers. The tag is a convenience for a client
// that wants to write conditionally, not a part of the representation, so
// losing it must not lose the orders.
func TestAPIOrdersAnswerWithoutATagWhenTaggingFails(t *testing.T) {
	store := apiReadStore()
	store.etagErr = errors.New("store unavailable")
	response := apiRead(t, store, "player", "/api/v1/orders")
	response.requireOK(t)
	if tag := response.Header().Get("ETag"); tag != "" {
		t.Fatalf("ETag = %q, want none", tag)
	}
}

// An If-Match this API cannot honour is refused rather than dropped. A
// precondition the server silently ignored would report success for exactly the
// write the client was trying to prevent.
func TestAPIOrderWritesRefuseAnIfMatchTheyCannotHonour(t *testing.T) {
	for _, header := range []string{
		`W/"abc"`,      // weak, and this comparison has to be strong
		`"abc", "def"`, // a list, and a write changes one resource
		`abc`,          // unquoted
		`""`,           // empty
		`"`,            // unterminated
	} {
		t.Run(header, func(t *testing.T) {
			store := apiReadStore()
			response := apiOrderWrite(t, store, http.MethodPost, "/api/v1/entities/7/orders",
				`{"turn":3,"kind":"move","detail":{"direction":"ne"}}`, header)
			assertAPIError(t, response, http.StatusBadRequest, apiCodeInvalidRequest)
			if store.wroteTurn != 0 {
				t.Fatal("the refused write reached the store")
			}
		})
	}
}

// `*` is "any current representation". These routes already require a
// configured faction on an open turn, so the condition is met by the time a
// write runs and the write proceeds.
func TestAPIOrderWritesAcceptIfMatchStar(t *testing.T) {
	store := apiReadStore()
	response := apiOrderWrite(t, store, http.MethodPost, "/api/v1/entities/7/orders",
		`{"turn":3,"kind":"move","detail":{"direction":"ne"}}`, "*")
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
}

// The whole point, at the HTTP layer: a stale tag is a 412 and writes nothing.
func TestAPIOrderWritesRefuseAStaleTag(t *testing.T) {
	const stale = `"0000000000000000000000000000000000000000000000000000000000000000"`
	for _, test := range []struct{ name, method, target, body string }{
		{name: "append", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":3,"kind":"move","detail":{"direction":"ne"}}`},
		{name: "insert", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":3,"sequence":1,"kind":"move","detail":{"direction":"ne"}}`},
		{name: "set one detail", method: http.MethodPatch, target: "/api/v1/entities/7/orders/1", body: `{"turn":3,"detail":{"direction":"e"}}`},
		{name: "set details", method: http.MethodPut, target: "/api/v1/orders", body: `{"turn":3,"updates":[{"entityId":7,"sequence":1,"detail":{"direction":"e"}}]}`},
		{name: "remove", method: http.MethodDelete, target: "/api/v1/entities/7/orders/1?turn=3", body: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := apiReadStore()
			before := len(store.orders[7])
			response := apiOrderWrite(t, store, test.method, test.target, test.body, stale)
			assertAPIError(t, response, http.StatusPreconditionFailed, apiCodePreconditionFailed)
			if len(store.orders[7]) != before {
				t.Fatalf("the refused %s changed the orders: %d, was %d", test.name, len(store.orders[7]), before)
			}
		})
	}
}

// A write carrying the tag it was given goes through, and the tag it gets back
// is good for the next one.
func TestAPIOrderWritesChainFromOneTagToTheNext(t *testing.T) {
	store := apiReadStore()
	tag := apiRead(t, store, "player", "/api/v1/orders").Header().Get("ETag")
	if tag == "" {
		t.Fatal("no tag to write against")
	}
	for step := range 3 {
		response := apiOrderWrite(t, store, http.MethodPost, "/api/v1/entities/7/orders",
			`{"turn":3,"kind":"move","detail":{"direction":"ne"}}`, tag)
		if response.Code != http.StatusCreated {
			t.Fatalf("write %d = %d %s", step, response.Code, response.Body.String())
		}
		next := response.Header().Get("ETag")
		if next == "" || next == tag {
			t.Fatalf("write %d returned tag %q, which is the one it was given", step, next)
		}
		tag = next
	}
}

// A write with no If-Match is the behaviour every client has today. This stays
// an opt-in, so nothing that worked before this stops working.
func TestAPIOrderWritesWithoutAnExpectationStillAppend(t *testing.T) {
	store := apiReadStore()
	before := len(store.orders[7])
	response := apiOrderWrite(t, store, http.MethodPost, "/api/v1/entities/7/orders",
		`{"turn":3,"kind":"move","detail":{"direction":"ne"}}`, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if len(store.orders[7]) != before+1 {
		t.Fatalf("orders = %d, want %d", len(store.orders[7]), before+1)
	}
}

// The scenario in issue #62, against the real store: one client reads the list
// and holds it while another appends. The stale client's append is refused
// instead of landing behind orders it never saw.
func TestAPIOrdersDetectAConcurrentEditAgainstTheRealStore(t *testing.T) {
	store, err := datastore.OpenMemory(t.Context(), datastore.Game{
		Seed1: 98374, Seed2: -98,
		Width: datastore.MinimumWorldWidth, Height: datastore.MinimumWorldHeight,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := newHandler(store.Authenticate, store)

	player := createParitySession(t, handler, "player@marajanda.com", "good.luck")
	headers := map[string]string{"Authorization": "Bearer " + player.Token}
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPut, "/api/v1/faction",
		`{"name":"The Wayfarers","race":"human"}`, headers), http.StatusOK)

	var entities apiEntities
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/entities", "", headers), &entities)
	var leader apiEntity
	for _, entity := range entities.Entities {
		if entity.Kind == "leader" {
			leader = entity
		}
	}
	orders := fmt.Sprintf("/api/v1/entities/%d/orders", leader.ID)

	// The first client reads, and holds what it read.
	read := apiRequest(handler, http.MethodGet, "/api/v1/orders", "", headers)
	held := read.Header().Get("ETag")
	if held == "" {
		t.Fatal("the orders read carried no tag")
	}

	// The second client - the orders page in another tab - writes three orders.
	for range 3 {
		assertParityMutationStatus(t, apiRequest(handler, http.MethodPost, orders,
			`{"turn":1,"kind":"rest","detail":{"count":1}}`, headers), http.StatusCreated)
	}

	// The first client appends against what it read, and is told no.
	conditional := map[string]string{"Authorization": "Bearer " + player.Token, "If-Match": held}
	refused := apiRequest(handler, http.MethodPost, orders,
		`{"turn":1,"kind":"move","detail":{"direction":"ne"}}`, conditional)
	assertAPIError(t, refused, http.StatusPreconditionFailed, apiCodePreconditionFailed)

	// Nothing was written, and the three orders it never saw are still three.
	var after apiOrders
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/orders", "", headers), &after)
	if got := len(parityEntityOrders(t, after, leader.ID).Orders); got != 3 {
		t.Fatalf("orders = %d, want the 3 that were there; the refused write landed", got)
	}

	// Reading again gives a tag that works, which is the whole recovery path.
	fresh := apiRequest(handler, http.MethodGet, "/api/v1/orders", "", headers).Header().Get("ETag")
	if fresh == held {
		t.Fatal("the tag did not move when the orders did")
	}
	retried := apiRequest(handler, http.MethodPost, orders,
		`{"turn":1,"kind":"move","detail":{"direction":"ne"}}`,
		map[string]string{"Authorization": "Bearer " + player.Token, "If-Match": fresh})
	assertParityMutationStatus(t, retried, http.StatusCreated)
}

// apiOrderWrite makes one authenticated order write, with If-Match when there
// is one to send.
func apiOrderWrite(t *testing.T, store *testStore, method, target, body, ifMatch string) *httptest.ResponseRecorder {
	t.Helper()
	token := make([]byte, datastore.SessionTokenBytes)
	store.sessionAccount = datastore.Account{
		Email: "player@example.com", Handle: "wanderer", Role: "player", Active: true,
		Seated: true, Origin: hexg.NewHex(2, -1),
	}
	store.sessions = map[string]bool{string(token): true}
	headers := map[string]string{
		"Authorization": "Bearer " + base64.RawURLEncoding.EncodeToString(token),
	}
	if body != "" {
		headers["Content-Type"] = apiJSONContentType
	}
	if ifMatch != "" {
		headers["If-Match"] = ifMatch
	}
	return apiRequest(newHandler(nil, store), method, target, body, headers)
}
