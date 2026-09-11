// Copyright (c) 2026 Michael D Henderson.

// These benchmarks are why the tag is a SHA-256 over an order list the caller
// already holds, rather than a cheaper hash or a second read. On an Apple M4:
// SHA-256 runs at 2.65 GB/s against djb2's 1.51, because djb2 is a serial
// dependency chain of one multiply-xor per byte while crypto/sha256 reaches the
// ARMv8 SHA instructions; and reading the order rows costs 35us against the
// 0.25us of hashing them, so a tag that went back for rows the handler already
// had would be a fifth of the cost of the whole request.

package datastore

import (
	"crypto/sha256"
	"testing"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
)

// benchPayload is the byte stream OrdersTag builds for a full order book.
func benchPayload() []byte {
	payload := make([]byte, 0, 1024)
	payload = append(payload, "orders/1\nfaction player@marajanda.com\nturn 1\nentity 2\n"...)
	for range MaxOrdersPerEntity {
		payload = append(payload, "order 32 move ne 0\n"...)
	}
	return payload
}

// benchStore seats a faction and gives its leader a full order book.
func benchStore(b *testing.B, orders int) (*Store, int64) {
	store, err := OpenMemory(b.Context(), testGame)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := store.SaveFaction(b.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
		b.Fatal(err)
	}
	entities, err := store.EntitiesAsOf(b.Context(), orderPlayer, game.FirstTurn)
	if err != nil {
		b.Fatal(err)
	}
	leader := entities[0]
	for range orders {
		if _, err := store.AddOrder(b.Context(), orderPlayer, game.FirstTurn, leader.ID,
			game.OrderKindMove, game.OrderDetail{Direction: compass.NE}); err != nil {
			b.Fatal(err)
		}
	}
	return store, leader.ID
}

// The hash alone, over a payload the size ordersETag actually builds.
func BenchmarkSHA256OverAFullOrderBook(b *testing.B) {
	payload := benchPayload()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		digest := sha256.New()
		digest.Write(payload)
		_ = digest.Sum(nil)
	}
}

// djb2, the classic: hash = hash*33 ^ c, seeded with 5381. 64-bit here, which
// is the friendliest version of it to compare against.
func djb2(payload []byte) uint64 {
	hash := uint64(5381)
	for _, c := range payload {
		hash = hash*33 ^ uint64(c)
	}
	return hash
}

func BenchmarkDJB2OverAFullOrderBook(b *testing.B) {
	payload := benchPayload()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		_ = djb2(payload)
	}
}

// The tag as it ships: hash an order list already in hand.
func BenchmarkOrdersTag(b *testing.B) {
	store, _ := benchStore(b, MaxOrdersPerEntity)
	defer store.Close()
	orders, err := store.OrdersAsOf(b.Context(), orderPlayer, game.FirstTurn)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = OrdersTag(orderPlayer, game.FirstTurn, orders)
	}
}

// What a GET /api/v1/orders costs today, for scale: the three reads the handler
// already makes before any tag is computed.
func BenchmarkOrdersReadHandlerWork(b *testing.B) {
	store, _ := benchStore(b, MaxOrdersPerEntity)
	defer store.Close()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.EntitiesAsOf(b.Context(), orderPlayer, game.FirstTurn); err != nil {
			b.Fatal(err)
		}
		if _, err := store.OrdersAsOf(b.Context(), orderPlayer, game.FirstTurn); err != nil {
			b.Fatal(err)
		}
		if _, err := store.EstimateOrders(b.Context(), orderPlayer, game.FirstTurn); err != nil {
			b.Fatal(err)
		}
	}
}
