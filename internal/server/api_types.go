// Copyright (c) 2026 Michael D Henderson.

package server

// These types are API DTOs: the explicit, versioned JSON boundary between the
// server's transport and its datastore/game values. Keeping them here prevents
// a JSON field change from becoming a game or storage change by accident.

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiCoordinate struct {
	Q int `json:"q"`
	R int `json:"r"`
}

type apiCreateSessionRequest struct {
	Email      string `json:"email"`
	Passphrase string `json:"passphrase"`
}

type apiSession struct {
	Token   string     `json:"token"`
	Account apiAccount `json:"account"`
}

type apiAccount struct {
	Email             string         `json:"email"`
	Handle            string         `json:"handle"`
	Role              string         `json:"role"`
	Active            bool           `json:"active"`
	Seated            bool           `json:"seated"`
	Origin            *apiCoordinate `json:"origin"`
	FactionConfigured bool           `json:"factionConfigured"`
}

type apiGame struct {
	CurrentTurn int     `json:"currentTurn"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Seeds       []int64 `json:"seeds,omitempty"`
}

type apiFaction struct {
	Name       string `json:"name"`
	Race       string `json:"race"`
	Active     bool   `json:"active"`
	Configured bool   `json:"configured"`
}

type apiPutFactionRequest struct {
	Name string `json:"name"`
	Race string `json:"race"`
}

type apiEntity struct {
	ID         int64         `json:"id"`
	Code       string        `json:"code"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Location   apiCoordinate `json:"location"`
	Allowance  int           `json:"allowance"`
	OrderKinds []string      `json:"orderKinds"`
}

type apiEntities struct {
	Turn     int         `json:"turn"`
	Entities []apiEntity `json:"entities"`
}

type apiHex struct {
	Coordinate apiCoordinate `json:"coordinate"`
	Terrain    string        `json:"terrain"`
	Elevation  int           `json:"elevation"`
}

type apiMap struct {
	Turn   int      `json:"turn"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Hexes  []apiHex `json:"hexes"`
}

type apiOrderDetail struct {
	Direction *string `json:"direction,omitempty"`
	Count     *int    `json:"count,omitempty"`
}

type apiOrder struct {
	Sequence int            `json:"sequence"`
	Kind     string         `json:"kind"`
	Detail   apiOrderDetail `json:"detail"`
}

type apiOrderCost struct {
	Sequence int    `json:"sequence"`
	Kind     string `json:"kind"`
	Cost     *int   `json:"cost"`
	Running  int    `json:"running"`
	Exhausts bool   `json:"exhausts"`
	// Warning is what the pre-processor expects to stop the order, in the
	// vocabulary a turn result reports afterwards, and null when it expects
	// nothing to. It is advice: the order is still priced, and to still names
	// where the step points.
	Warning *string       `json:"warning"`
	From    apiCoordinate `json:"from"`
	Target  apiCoordinate `json:"target"`
	To      apiCoordinate `json:"to"`
}

type apiOrderEstimate struct {
	Allowance  int            `json:"allowance"`
	Committed  int            `json:"committed"`
	Total      int            `json:"total"`
	Residue    int            `json:"residue"`
	Overspend  int            `json:"overspend"`
	ExhaustsAt *int           `json:"exhaustsAt"`
	End        apiCoordinate  `json:"end"`
	Orders     []apiOrderCost `json:"orders"`
}

type apiEntityOrders struct {
	EntityID int64            `json:"entityId"`
	Orders   []apiOrder       `json:"orders"`
	Estimate apiOrderEstimate `json:"estimate"`
}

type apiOrders struct {
	Turn     int               `json:"turn"`
	Entities []apiEntityOrders `json:"entities"`
}

// The turn report. One entity's recorded turn on the three grains the record
// keeps it on: the ledger of its action points, an outcome per order, and an
// observation per hex an order revealed. See docs/reference/turn-results.md.

type apiResultLedger struct {
	Allowance int           `json:"allowance"`
	Spent     int           `json:"spent"`
	Lapsed    int           `json:"lapsed"`
	Start     apiCoordinate `json:"start"`
	End       apiCoordinate `json:"end"`
}

type apiOrderOutcome struct {
	Sequence int    `json:"sequence"`
	Kind     string `json:"kind"`
	// Cost is what the entity was charged, which is not what the order would
	// have cost: an order it could not afford charges nothing, and a step that
	// failed on terrain is charged in full.
	Cost    int  `json:"cost"`
	Carried bool `json:"carried"`
	// Reason is why the order did not happen, from the recorded vocabulary, and
	// null for an order that was carried out.
	Reason *string       `json:"reason"`
	From   apiCoordinate `json:"from"`
	Target apiCoordinate `json:"target"`
	To     apiCoordinate `json:"to"`
}

type apiObservation struct {
	// Sequence is the order that revealed the hex.
	Sequence   int           `json:"sequence"`
	Coordinate apiCoordinate `json:"coordinate"`
	State      string        `json:"state"`
}

type apiEntityResult struct {
	EntityID     int64             `json:"entityId"`
	Ledger       apiResultLedger   `json:"ledger"`
	Orders       []apiOrderOutcome `json:"orders"`
	Observations []apiObservation  `json:"observations"`
}

type apiResults struct {
	// Turn is the turn reported. It is game.StartOfTimeTurn - zero - on the
	// unscoped read of a game that has processed no turn yet.
	Turn     int               `json:"turn"`
	Entities []apiEntityResult `json:"entities"`
}

type apiCreateOrderRequest struct {
	Turn     int             `json:"turn"`
	Sequence *int            `json:"sequence,omitempty"`
	Kind     string          `json:"kind"`
	Detail   *apiOrderDetail `json:"detail"`
}

type apiSetOrderDetailRequest struct {
	Turn   int             `json:"turn"`
	Detail *apiOrderDetail `json:"detail"`
}

type apiOrderUpdate struct {
	EntityID int64           `json:"entityId"`
	Sequence int             `json:"sequence"`
	Detail   *apiOrderDetail `json:"detail"`
}

type apiSetOrderDetailsRequest struct {
	Turn    int              `json:"turn"`
	Updates []apiOrderUpdate `json:"updates"`
}

// apiReplacementOrder is one order in a declared list. It carries no sequence:
// position in the list is the sequence, which is what makes the same request
// sent twice mean the same thing.
type apiReplacementOrder struct {
	Kind   string          `json:"kind"`
	Detail *apiOrderDetail `json:"detail"`
}

type apiReplaceOrdersRequest struct {
	Turn   int                    `json:"turn"`
	Orders *[]apiReplacementOrder `json:"orders"`
}

type apiOrderMutation struct {
	Turn     int   `json:"turn"`
	EntityID int64 `json:"entityId"`
	// Sequence is the one order a write addressed. A whole-list write
	// addresses none, so it is absent rather than reported as zero.
	Sequence *int             `json:"sequence,omitempty"`
	Orders   []apiOrder       `json:"orders"`
	Estimate apiOrderEstimate `json:"estimate"`
}

type apiTurn struct {
	Turn int `json:"turn"`
}
