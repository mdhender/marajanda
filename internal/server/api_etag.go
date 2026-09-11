// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"
	"strings"

	"github.com/mdhender/marajanda/internal/datastore"
)

// The orders concurrency tag. Two clients can hold the same turn's order list -
// the orders page in one tab and a script in another is ordinary for a
// play-by-mail game - and nothing in an append told either that the other had
// written. See issue #62.
//
// Every orders representation carries an ETag, and an order write may carry
// `If-Match` to say which list it believed it was writing to. A write whose
// expectation no longer holds is refused with 412 and changes nothing, so a
// client can say "append this, assuming the list is still the three orders I
// read" and find out when it is wrong.
//
// `If-Match` is optional. A write without one appends to whatever is there,
// which is what every client does today; this is a capability to opt into
// rather than a new requirement. The consequence is that a client that sends no
// expectation can still clobber one that did, which is why the orders page is
// still the client that always wins.

// setAPIOrdersETag puts the tag for the faction's orders on the response.
//
// It takes the orders the handler has already read rather than reading them
// again. Hashing them is a fraction of a microsecond; a second trip for the
// same rows would be a fifth of the cost of the whole request.
func setAPIOrdersETag(w http.ResponseWriter, r *http.Request, turn int, orders map[int64][]datastore.Order) {
	w.Header().Set("ETag", `"`+datastore.OrdersTag(apiPlayerEmail(r), turn, orders)+`"`)
}

// apiOrderExpectation reads `If-Match` into the store options a write carries.
// It answers the request itself and reports false when the header is one this
// API cannot honour, because a precondition the server quietly dropped is worse
// than no precondition at all.
func apiOrderExpectation(w http.ResponseWriter, r *http.Request) ([]datastore.OrderWriteOption, bool) {
	header := strings.TrimSpace(r.Header.Get("If-Match"))
	switch {
	case header == "":
		return nil, true
	case header == "*":
		// "any current representation". The routes that accept this header all
		// require a configured faction on an open turn, so by the time a write
		// runs there is one, and the condition is already met.
		return nil, true
	}
	tag, ok := parseAPIETag(header)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest,
			"If-Match must be one strong entity-tag from an orders response, or *.")
		return nil, false
	}
	return []datastore.OrderWriteOption{datastore.ExpectOrders(tag)}, true
}

// parseAPIETag unwraps one quoted entity-tag.
//
// A list is refused rather than searched. A write changes one resource, so the
// several tags a list offers cannot all be the one it is writing to, and a
// server that picked one would be guessing. A weak tag (`W/"..."`) is refused
// for the reason If-Match exists: weak comparison admits representations that
// differ, and this comparison decides whether somebody else's orders are about
// to be written over.
func parseAPIETag(value string) (string, bool) {
	if strings.Contains(value, ",") || strings.HasPrefix(value, "W/") {
		return "", false
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	tag := value[1 : len(value)-1]
	if tag == "" {
		return "", false
	}
	return tag, true
}
