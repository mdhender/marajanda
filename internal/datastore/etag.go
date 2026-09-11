// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
)

// ordersETagVersion prefixes the hashed bytes so the encoding below can change
// without a new tag colliding with an old one a client still holds. Bump it
// when what is hashed changes; nothing stores a tag, so nothing migrates.
const ordersETagVersion = "orders/1"

// OrdersETag returns an opaque tag for the orders a faction holds on a turn.
//
// The tag is derived, not stored: it is a hash of the order list itself, so it
// costs a read and no schema. Two reads of an unchanged list produce the same
// tag on any machine and after any restart, and any change to any order of any
// of the faction's entities produces a different one.
//
// It covers the orders alone, not the estimate served beside them, and that is
// enough to identify the whole representation. An estimate is a function of the
// orders, the entities and what the faction knows; within one turn the last two
// do not move, because turn processing dates everything it writes from turn+1
// and a turn the game has left refuses writes. The turn is hashed in, so a
// representation that changes because the clock moved gets a new tag too.
func (s *Store) OrdersETag(ctx context.Context, email string, turn int) (string, error) {
	if !game.ValidTurn(turn) {
		return "", fmt.Errorf("tag orders: %d is not a turn", turn)
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	return ordersETag(conn, normalizeEmail(email), turn)
}

// ordersETag hashes the faction's orders for one turn on a connection the
// caller holds, so a write can compare against it inside its own transaction
// rather than reading the list, letting go, and hoping.
func ordersETag(conn *sqlite.Conn, normalizedEmail string, turn int) (string, error) {
	orders, err := readFactionOrders(conn, normalizedEmail, turn)
	if err != nil {
		return "", err
	}
	// Entity order is the hash's own, not the map's: Go randomizes map
	// iteration, and a tag that depended on it would differ between two reads
	// of one unchanged list. readFactionOrders already returns each entity's
	// orders in sequence order.
	entityIDs := make([]int64, 0, len(orders))
	for entityID := range orders {
		entityIDs = append(entityIDs, entityID)
	}
	sort.Slice(entityIDs, func(i, j int) bool { return entityIDs[i] < entityIDs[j] })

	digest := sha256.New()
	writeETagLine(digest, "%s\n", ordersETagVersion)
	writeETagLine(digest, "faction %s\n", normalizedEmail)
	writeETagLine(digest, "turn %d\n", turn)
	for _, entityID := range entityIDs {
		writeETagLine(digest, "entity %d\n", entityID)
		for _, order := range orders[entityID] {
			// Both halves of the detail are written for every order, so a
			// rest with count 2 and a move that somehow held count 2 could
			// not hash alike.
			writeETagLine(digest, "order %d %s %s %d\n",
				order.Seq, order.Kind, storedDirection(order.Detail.Direction), order.Detail.Count)
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// writeETagLine feeds one line to the digest. A hash.Hash never returns an
// error from Write, which is why there is nothing here to report.
func writeETagLine(digest io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(digest, format, args...)
}
