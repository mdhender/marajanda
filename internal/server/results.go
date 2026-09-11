// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// The results page: what a processed turn actually did.
//
// The orders page warns before the turn and this is what closes the loop after
// it. A player whose leader stopped two hexes short saw the plan and then saw
// the ending hex, and had to infer everything between them; the record has
// carried the answer since #33, and nothing read it back. See issue #59 and
// docs/reference/turn-results.md.
//
// The page reports rather than explains. The words below are the failure
// vocabulary written out as sentences, and nothing here decides anything: a
// reason this page has no sentence for is printed with the word the turn
// recorded rather than swallowed.

// resultsPath is the page's own URL, which its turn links are built from.
const resultsPath = "/player/results"

// resultsTurnParam is how the page names the turn it is reading.
//
// It is the API's spelling, not `turn`. A read that names a past turn says
// asOfTurn everywhere in this project, leaving `turn` to mean on a write the
// turn the client believed it was writing to. One vocabulary, both transports.
const resultsTurnParam = "asOfTurn"

// resultsView is one turn's account, ready for the template.
type resultsView struct {
	// Turn is the turn being reported, or zero before the game has processed
	// one. Zero is game.StartOfTimeTurn: there is no turn to report rather
	// than a turn that reported nothing.
	Turn int
	// Latest is the newest turn there is a report for, which is the turn the
	// page opens on.
	Latest int
	// Newer and Older are the turns either side of this one. Either is nil at
	// its end of the range, and both are nil when there is one report.
	Newer *resultsTurnLink
	Older *resultsTurnLink
	// Entities is the faction's force, one section each, in the order the
	// record answers with.
	Entities []entityResult
	// Message is what the page has to say about the turn it is showing: that
	// nothing has been processed yet, or that the turn asked for is not one
	// the game has a report for.
	Message string
}

// resultsTurnLink is one step along the turns, as a link that works without
// script and as an HTMX swap when there is one.
type resultsTurnLink struct {
	Turn  int
	URL   string
	Label string
}

// entityResult is one entity's section: its ledger, and what each of its
// orders did.
type entityResult struct {
	Entity datastore.Entity
	// Ledger is the action points of the turn. It is rendered for every entity
	// including the ones that account for nothing, because a player reads
	// their whole force in one place.
	Ledger resultLedger
	// Summary is the section's opening sentence: what this entity's turn was,
	// in a line. An entity that takes no orders says so instead of showing a
	// row of zeroes.
	Summary string
	// Orders is one line per order the entity carried.
	Orders []orderOutcome
	// TakesOrders is false for an entity kind that accepts none. Its section
	// is its summary and nothing else.
	TakesOrders bool
}

// resultLedger is the ledger line: the points, and where the entity stood at
// each end of the turn.
type resultLedger struct {
	Allowance int
	Spent     int
	Lapsed    int
	Start     string
	End       string
	// Moved reports that the turn ended somewhere other than it started, which
	// is what decides whether the line reads as a journey or as a standstill.
	Moved bool
}

// orderOutcome is one order as the turn resolved it.
//
// Three coordinates rather than one: a plan that went sideways at step two is
// only legible when each order says where it resolved from, where it was
// aimed, and where it left the entity.
type orderOutcome struct {
	Seq     int
	Label   string
	Cost    string
	Carried bool
	// Reason is why the order did not happen, as a sentence. It is empty for
	// an order that was carried out.
	Reason string
	From   string
	Target string
	To     string
	// Aimed reports that this order was aimed somewhere, which is what decides
	// whether the line names a target at all. A rest aims nowhere.
	Aimed bool
	// Revealed is what this order added to the map, counted: a list of
	// coordinates is faithful and unreadable, so the count is the line and the
	// coordinates are behind it.
	Revealed string
	// Observations are the hexes this order revealed, in the order the record
	// answers with. They are drawn inside a disclosure, so the page is a report
	// at a glance and a list of hexes when a player wants one.
	Observations []observedHex
}

// observedHex is one hex an order revealed, and the state it was revealed in.
type observedHex struct {
	Coord string
	State string
}

// results renders the report of one processed turn.
func (app *application) results(w http.ResponseWriter, r *http.Request) {
	account, faction, ok := app.playerReadingFaction(w, r)
	if !ok {
		return
	}
	current, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load the turn.")
		return
	}
	// The turn worth opening on is the one just processed, which is the turn
	// before the one being ordered. A game on its first turn has none, and the
	// page says so rather than looking broken.
	latest := current - 1

	turn, status, message := resultsTurn(r, latest)
	view := resultsView{Turn: turn, Latest: latest, Message: message}
	if game.ValidTurn(turn) {
		results, err := app.store.ResultsAsOf(r.Context(), account.Email, turn)
		if err != nil {
			app.serverError(w, r, err, "Marajanda could not load that turn's report.")
			return
		}
		// The entities are read as of the turn reported, so a section is
		// headed with the name and kind the entity had then rather than the
		// one it has now.
		entities, err := app.store.EntitiesAsOf(r.Context(), account.Email, turn)
		if err != nil {
			app.serverError(w, r, err, "Marajanda could not load your force.")
			return
		}
		view.Entities = buildEntityResults(results, entities)
		view.Newer, view.Older = resultsTurnLinks(turn, latest)
	}

	// One URL, two shapes of answer, so the response says what it varied on -
	// the same reason the map region and the orders list do.
	w.Header().Set("Vary", "HX-Request")
	data := pageData{
		Title:   "Turn report",
		View:    "results",
		Account: account,
		Faction: faction,
		Turn:    current,
		Results: view,
	}
	if wantsFragment(r) {
		app.renderFragment(w, status, "results-region", data)
		return
	}
	app.render(w, status, data)
}

// playerReadingFaction resolves the faction of a player who is reading rather
// than acting.
//
// It is deliberately not the orders page's playerFaction, which sends a
// deactivated faction away. The flag stops a faction acting; it does not lock a
// person out of reading what their own people already did, which is the same
// rule the player map follows.
func (app *application) playerReadingFaction(w http.ResponseWriter, r *http.Request) (datastore.Account, datastore.Faction, bool) {
	account, ok := app.requireRole(w, r, "player")
	if !ok {
		return datastore.Account{}, datastore.Faction{}, false
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load your faction.")
		return datastore.Account{}, datastore.Faction{}, false
	}
	if !found || !faction.Configured() {
		redirectPlayer(w, r, "/player/faction")
		return datastore.Account{}, datastore.Faction{}, false
	}
	return account, faction, true
}

// resultsTurn decides which turn the page draws, and what it has to say about
// the request that asked for it.
//
// Three answers, and they are the API's three: a turn the game has a report
// for, a well-formed turn it does not, and something that is not a turn at all.
// A refusal still draws the latest report rather than an empty page, because a
// mistyped link is not a reason to show a player nothing.
func resultsTurn(r *http.Request, latest int) (turn, status int, message string) {
	if latest < game.FirstTurn {
		return 0, http.StatusOK, "No turn has been processed yet. This report opens once the first turn is closed."
	}
	value := strings.TrimSpace(r.URL.Query().Get(resultsTurnParam))
	if value == "" {
		return latest, http.StatusOK, ""
	}
	asked, err := strconv.Atoi(value)
	if err != nil {
		return latest, http.StatusBadRequest, "That is not a turn. Showing the most recent report instead."
	}
	if asked < game.FirstTurn || asked > latest {
		return latest, http.StatusNotFound, fmt.Sprintf("There is no report for turn %d. Showing the most recent one instead.", asked)
	}
	return asked, http.StatusOK, ""
}

// resultsTurnLinks are the steps either side of the turn being read.
//
// Newer is the higher turn, and it is nil on the latest report; older is nil on
// the first. Both keep an href, so the page walks the turns with the script
// blocked exactly as it does with it loaded.
func resultsTurnLinks(turn, latest int) (newer, older *resultsTurnLink) {
	if turn < latest {
		newer = &resultsTurnLink{Turn: turn + 1, URL: resultsTurnURL(turn + 1), Label: fmt.Sprintf("Turn %d", turn+1)}
	}
	if turn > game.FirstTurn {
		older = &resultsTurnLink{Turn: turn - 1, URL: resultsTurnURL(turn - 1), Label: fmt.Sprintf("Turn %d", turn-1)}
	}
	return newer, older
}

func resultsTurnURL(turn int) string {
	return fmt.Sprintf("%s?%s=%d", resultsPath, resultsTurnParam, turn)
}

// buildEntityResults pairs each recorded result with the entity it belongs to.
//
// The record is the order of the page: results answer in creation order, which
// is the order the dashboard and the orders page list a force in. An entity
// with no row as of the turn is still reported - the result is the fact, and a
// section headed by its identifier says more than a dropped section.
func buildEntityResults(results []datastore.TurnResult, entities []datastore.Entity) []entityResult {
	known := make(map[int64]datastore.Entity, len(entities))
	for _, entity := range entities {
		known[entity.ID] = entity
	}
	sections := make([]entityResult, 0, len(results))
	for _, result := range results {
		entity, found := known[result.EntityID]
		if !found {
			entity = datastore.Entity{ID: result.EntityID, Code: fmt.Sprintf("ENTITY-%d", result.EntityID)}
			entity.Name = entity.Code
		}
		section := entityResult{
			Entity:      entity,
			Ledger:      buildResultLedger(result),
			TakesOrders: len(entity.Kind.OrderKinds()) > 0,
			Orders:      buildOrderOutcomes(result),
		}
		section.Summary = resultSummary(section)
		sections = append(sections, section)
	}
	return sections
}

func buildResultLedger(result datastore.TurnResult) resultLedger {
	return resultLedger{
		Allowance: result.Allowance,
		Spent:     result.Spent,
		Lapsed:    result.Lapsed,
		Start:     coordLabel(result.Start),
		End:       coordLabel(result.End),
		Moved:     result.Start != result.End,
	}
}

// resultSummary is the sentence at the top of an entity's section.
//
// Four turns a player asks a different question about: an entity that takes no
// orders, one that was given none, one that spent everything, and one that has
// points left over. Each is a line rather than a table to read the difference
// out of.
func resultSummary(section entityResult) string {
	ledger := section.Ledger
	switch {
	case !section.TakesOrders:
		return fmt.Sprintf("A %s takes no orders, so it has no account to give.", section.Entity.Kind)
	case len(section.Orders) == 0:
		return fmt.Sprintf("No orders were given, so all %s lapsed.", actionPoints(ledger.Allowance))
	case ledger.Lapsed == 0:
		return fmt.Sprintf("Spent every one of its %s.", actionPoints(ledger.Allowance))
	default:
		return fmt.Sprintf("Spent %d of its %s; %s lapsed.", ledger.Spent, actionPoints(ledger.Allowance), actionPoints(ledger.Lapsed))
	}
}

// buildOrderOutcomes is the entity's orders, each with the hexes it revealed
// hung underneath it.
//
// Observations are recorded per order, so they are reported per order. An
// observation naming an order the result does not carry is not dropped: it is
// collected under the order it names, and an outcome is made for it rather than
// losing a sighting to a record that disagrees with itself.
func buildOrderOutcomes(result datastore.TurnResult) []orderOutcome {
	revealed := make(map[int][]game.Observation)
	for _, observation := range result.Observations {
		revealed[observation.Seq] = append(revealed[observation.Seq], observation)
	}
	outcomes := make([]orderOutcome, 0, len(result.Orders))
	for _, order := range result.Orders {
		outcome := orderOutcome{
			Seq:     order.Seq,
			Label:   orderKindLabel(order.Kind),
			Cost:    fmt.Sprintf("%d AP", order.Cost),
			Carried: order.Carried,
			Reason:  resultReasonLabel(order.Reason),
			From:    coordLabel(order.From),
			Target:  coordLabel(order.Target),
			To:      coordLabel(order.To),
			Aimed:   order.Target != order.From,
		}
		outcome.Observations = observedHexes(revealed[order.Seq])
		outcome.Revealed = revealedLabel(revealed[order.Seq])
		delete(revealed, order.Seq)
		outcomes = append(outcomes, outcome)
	}
	for seq, observations := range revealed {
		outcomes = append(outcomes, orderOutcome{
			Seq:          seq,
			Label:        "Order",
			Cost:         "—",
			Carried:      true,
			Observations: observedHexes(observations),
			Revealed:     revealedLabel(observations),
		})
	}
	return outcomes
}

func observedHexes(observations []game.Observation) []observedHex {
	if len(observations) == 0 {
		return nil
	}
	hexes := make([]observedHex, 0, len(observations))
	for _, observation := range observations {
		hexes = append(hexes, observedHex{Coord: coordLabel(observation.Hex), State: string(observation.State)})
	}
	return hexes
}

// revealedLabel counts what an order added to the map, in the two states the
// knowledge record keeps.
//
// The distinction is a game concept - a hex your people walked through is not a
// hex they glimpsed from a ridge - so the count names both rather than totalling
// them away. See docs/reference/knowledge.md.
func revealedLabel(observations []game.Observation) string {
	if len(observations) == 0 {
		return ""
	}
	var explored, observed int
	for _, observation := range observations {
		if observation.State == game.KnowledgeExplored {
			explored++
			continue
		}
		observed++
	}
	parts := make([]string, 0, 2)
	if explored > 0 {
		parts = append(parts, fmt.Sprintf("%d explored", explored))
	}
	if observed > 0 {
		parts = append(parts, fmt.Sprintf("%d observed", observed))
	}
	return fmt.Sprintf("Revealed %s: %s", hexCount(len(observations)), strings.Join(parts, ", "))
}

func hexCount(count int) string {
	if count == 1 {
		return "1 hex"
	}
	return fmt.Sprintf("%d hexes", count)
}

// resultReasonLabel is why an order did not happen, in the words a player
// reads.
//
// The record keeps a four-value vocabulary and a page owes a sentence. A reason
// this has no sentence for is printed with the word the turn recorded: a
// failure nobody has written a line for yet is still a failure the player is
// entitled to see.
func resultReasonLabel(reason game.FailureReason) string {
	switch reason {
	case "":
		return ""
	case game.FailureTerrain:
		return "It could not enter that hex."
	case game.FailureExhaust:
		return "It had run out of action points."
	case game.FailureUnknown:
		return "The order never said which way to go."
	default:
		// `blocked` is in the vocabulary and nothing produces it, so its
		// sentence arrives with the rule that earns it rather than being
		// guessed at here. Until then the word is reported as recorded.
		return fmt.Sprintf("It did not happen, and the turn recorded %q.", string(reason))
	}
}
