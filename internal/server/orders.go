// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// The names the orders form posts.
//
// A step box carries its whole address in its name - entity, stanza and step -
// because one control has to serve two pages. With script, the box posts itself
// to a URL that names the same box and the server sets that one box; without
// script, the same boxes are submitted together by the page's one Save button,
// and then the name is the only thing that says which box a value belongs to.
//
// The address is in the URL as well as the name so that a scripted write is
// still addressed by its URL: HTMX includes the whole enclosing form on a
// non-GET request, so the request that changes one box carries every other box
// with it, and the URL is what says which of them was touched.
const (
	stepField   = "step"   // step.<entity>.<seq>.<step>
	kindField   = "kind"   // kind.<entity>
	addField    = "add"    // the add button's value: <entity>
	removeField = "remove" // the remove button's value: <entity>.<seq>
)

// ordersView is the orders page, ready for the template.
type ordersView struct {
	Turn     int
	Entities []entityOrders
	// Directions are the options every step box offers, in compass order.
	Directions []orderDirection
	// Saved is the time the last write landed, or empty on a page that has not
	// written anything. Feedback rides in the fragment because the fragment is
	// the only thing a scripted save replaces.
	Saved string
	// Message is a failure that belongs to the page rather than to one stanza:
	// a turn that closed, a form that could not be read.
	Message string
}

// entityOrders is one entity's section of the page: what it is, what it has
// been told to do, and what else it can be told.
type entityOrders struct {
	Entity  datastore.Entity
	Stanzas []orderStanza
	// Kinds are the order kinds this entity's kind accepts. Empty means it
	// accepts none, and the section says so rather than being left out: a
	// player sees their whole force in one place.
	Kinds     []orderKindOption
	KindField string
	AddValue  string
}

// orderKindOption is one choice in an entity's "add order" control.
type orderKindOption struct {
	Value string
	Label string
}

// orderStanza is one order: its kind, its step boxes, and the control that
// removes it.
type orderStanza struct {
	Seq         int
	Kind        string
	Label       string
	Boxes       []orderBox
	RemoveURL   string
	RemoveValue string
	// Error is a failure that belongs to this stanza, shown beside it.
	Error string
}

// orderBox is one step box: a select that is either a stored step or the blank
// one on the end.
//
// The count is always steps plus one, so there is no "add a box" control and no
// fixed number of boxes anywhere in the code.
type orderBox struct {
	Step    int
	Name    string
	Post    string
	Current string
	Label   string
}

// orderDirection is one option of a step box.
type orderDirection struct {
	Value string
	Label string
}

// orderFeedback is what a write has to say for itself on the way back.
type orderFeedback struct {
	saved bool
	// message is the failure, and entity and seq say which stanza it belongs
	// to. A failure with no stanza is shown at the top of the page.
	message string
	entity  int64
	seq     int
	status  int
}

// ordersPath is where an unscripted write is sent back to.
const ordersPath = "/player/orders"

// orders renders the page a player builds their turn on.
func (app *application) orders(w http.ResponseWriter, r *http.Request) {
	account, faction, ok := app.playerFaction(w, r)
	if !ok {
		return
	}
	app.renderOrders(w, r, account, faction, orderFeedback{})
}

// saveOrders is the whole form: every step box, and at most one button.
//
// It is what the script-free page's Save button posts, and it is also where the
// add control goes, scripted or not. The steps are applied first and the button
// afterwards, so a player who fills in a box and presses "add order" in one
// unscripted submission keeps both.
func (app *application) saveOrders(w http.ResponseWriter, r *http.Request) {
	account, faction, ok := app.playerFaction(w, r)
	if !ok {
		return
	}
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load your orders.", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.renderOrders(w, r, account, faction, orderFeedback{
			message: "Marajanda could not read that form.", status: http.StatusBadRequest,
		})
		return
	}
	stanzas, err := parseStepFields(r.PostForm)
	if err != nil {
		app.renderOrders(w, r, account, faction, orderFeedback{
			message: "Marajanda could not read those orders.", status: http.StatusBadRequest,
		})
		return
	}
	if err := app.store.SetOrderSteps(r.Context(), account.Email, turn, stanzas); err != nil {
		app.renderOrders(w, r, account, faction, orderWriteFeedback(err, 0, 0))
		return
	}
	switch add, remove := r.PostForm.Get(addField), r.PostForm.Get(removeField); {
	case add != "":
		entity, err := strconv.ParseInt(add, 10, 64)
		if err != nil {
			app.renderOrders(w, r, account, faction, orderFeedback{
				message: "Marajanda could not read that entity.", status: http.StatusBadRequest,
			})
			return
		}
		kind := game.OrderKind(strings.ToLower(strings.TrimSpace(r.PostForm.Get(kindField + "." + add))))
		if _, err := app.store.AddOrder(r.Context(), account.Email, turn, entity, kind); err != nil {
			app.renderOrders(w, r, account, faction, orderWriteFeedback(err, entity, 0))
			return
		}
	case remove != "":
		entity, seq, err := parseStanzaAddress(remove)
		if err != nil {
			app.renderOrders(w, r, account, faction, orderFeedback{
				message: "Marajanda could not read that order.", status: http.StatusBadRequest,
			})
			return
		}
		if err := app.store.RemoveOrder(r.Context(), account.Email, turn, entity, seq); err != nil {
			app.renderOrders(w, r, account, faction, orderWriteFeedback(err, entity, seq))
			return
		}
	}
	app.renderOrders(w, r, account, faction, orderFeedback{saved: true})
}

// setOrderStep sets one step box. The box is addressed by the URL, and its
// value arrives under the name that addresses it.
//
// HTMX sends the whole enclosing form with the request, so the other boxes are
// in it too. They are ignored: this route changes the one box it names, and
// every other box was saved when it changed.
func (app *application) setOrderStep(w http.ResponseWriter, r *http.Request) {
	account, faction, ok := app.playerFaction(w, r)
	if !ok {
		return
	}
	entity, seq, err := pathStanza(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	step, err := strconv.Atoi(r.PathValue("step"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load your orders.", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.renderOrders(w, r, account, faction, orderFeedback{
			message: "Marajanda could not read that form.", status: http.StatusBadRequest,
		})
		return
	}
	direction, err := parseDirection(r.PostForm.Get(stepFieldName(entity, seq, step)))
	if err != nil {
		app.renderOrders(w, r, account, faction, orderFeedback{
			message: err.Error(), entity: entity, seq: seq, status: http.StatusUnprocessableEntity,
		})
		return
	}
	if err := app.store.SetOrderStep(r.Context(), account.Email, turn, entity, seq, step, direction); err != nil {
		app.renderOrders(w, r, account, faction, orderWriteFeedback(err, entity, seq))
		return
	}
	app.renderOrders(w, r, account, faction, orderFeedback{saved: true})
}

// removeOrder removes one stanza. The unscripted page reaches the same work
// through the remove button on the form, which a browser can submit and a
// DELETE it cannot.
func (app *application) removeOrder(w http.ResponseWriter, r *http.Request) {
	account, faction, ok := app.playerFaction(w, r)
	if !ok {
		return
	}
	entity, seq, err := pathStanza(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load your orders.", http.StatusInternalServerError)
		return
	}
	if err := app.store.RemoveOrder(r.Context(), account.Email, turn, entity, seq); err != nil {
		app.renderOrders(w, r, account, faction, orderWriteFeedback(err, entity, seq))
		return
	}
	app.renderOrders(w, r, account, faction, orderFeedback{saved: true})
}

// advanceTurn moves the game's clock on by one.
//
// That is all it does. The orders of the turn it leaves behind are frozen by
// the move itself - every write checks the current turn - and processing them
// is separate work.
func (app *application) advanceTurn(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.requireRole(w, r, "admin"); !ok {
		return
	}
	if _, err := app.store.AdvanceTurn(r.Context()); err != nil {
		http.Error(w, "Marajanda could not advance the turn.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

// playerFaction resolves the session's player and confirms it has a faction to
// give orders to, reusing the gate the dashboard uses.
func (app *application) playerFaction(w http.ResponseWriter, r *http.Request) (datastore.Account, datastore.Faction, bool) {
	account, ok := app.requirePlayer(w, r)
	if !ok {
		return datastore.Account{}, datastore.Faction{}, false
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
		return datastore.Account{}, datastore.Faction{}, false
	}
	if !found || !faction.Configured() {
		http.Redirect(w, r, "/player/faction", http.StatusSeeOther)
		return datastore.Account{}, datastore.Faction{}, false
	}
	return account, faction, true
}

// renderOrders reads the turn back and draws the page from it.
//
// Every write answers with the whole re-rendered orders region rather than with
// the control that was touched, so numbering, compaction and validation are
// decided by the server and there is no client-side state to drift.
//
// A scripted write always answers 200. HTMX does not swap the response of a
// failed request, and an inline error that is never swapped in is an error
// nobody reads; the fragment carries the message instead. An unscripted request
// gets the page and the status the failure deserves.
func (app *application) renderOrders(w http.ResponseWriter, r *http.Request, account datastore.Account, faction datastore.Faction, feedback orderFeedback) {
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load your orders.", http.StatusInternalServerError)
		return
	}
	entities, err := app.store.EntitiesAsOf(r.Context(), account.Email, turn)
	if err != nil {
		http.Error(w, "Marajanda could not load your force.", http.StatusInternalServerError)
		return
	}
	orders, err := app.store.OrdersAsOf(r.Context(), account.Email, turn)
	if err != nil {
		http.Error(w, "Marajanda could not load your orders.", http.StatusInternalServerError)
		return
	}
	// One URL, two shapes of answer, so the response says what it varied on -
	// the same reason the map region does.
	w.Header().Set("Vary", "HX-Request")
	data := pageData{
		Title:   "Orders",
		View:    "orders",
		Account: account,
		Faction: faction,
		Turn:    turn,
		Orders:  buildOrdersView(turn, entities, orders, feedback),
	}
	if wantsFragment(r) {
		app.renderFragment(w, http.StatusOK, "orders-list", data)
		return
	}
	// A write that landed and was not asked for as a fragment came from the
	// page's own Save button, so it is answered the way a form submission
	// should be: a redirect, and a refresh that reloads rather than saves
	// again.
	if feedback.saved && r.Method != http.MethodGet {
		http.Redirect(w, r, ordersPath, http.StatusSeeOther)
		return
	}
	status := http.StatusOK
	if feedback.status != 0 {
		status = feedback.status
	}
	app.render(w, status, data)
}

// buildOrdersView turns a turn's entities and their orders into the page.
//
// Every entity the faction owns gets a section, in the order the force is
// listed, whether or not it can be given an order. A player sees their whole
// force in one place rather than wondering what happened to their hamlet.
func buildOrdersView(turn int, entities []datastore.Entity, orders map[int64][]datastore.Order, feedback orderFeedback) ordersView {
	view := ordersView{Turn: turn, Directions: orderDirections()}
	if feedback.saved {
		view.Saved = time.Now().UTC().Format("15:04:05 MST")
	}
	attached := false
	for _, entity := range entities {
		section := entityOrders{
			Entity:    entity,
			KindField: fmt.Sprintf("%s.%d", kindField, entity.ID),
			AddValue:  strconv.FormatInt(entity.ID, 10),
		}
		for _, kind := range entity.Kind.OrderKinds() {
			section.Kinds = append(section.Kinds, orderKindOption{Value: string(kind), Label: orderKindLabel(kind)})
		}
		for _, order := range orders[entity.ID] {
			stanza := buildStanza(entity.ID, order, feedback)
			attached = attached || stanza.Error != ""
			section.Stanzas = append(section.Stanzas, stanza)
		}
		view.Entities = append(view.Entities, section)
	}
	// A failure that belongs to no stanza on the page - one that names a
	// stanza that has just been removed, or one about the page itself - still
	// has to be read somewhere.
	if !attached {
		view.Message = feedback.message
	}
	return view
}

func buildStanza(entityID int64, order datastore.Order, feedback orderFeedback) orderStanza {
	stanza := orderStanza{
		Seq:         order.Seq,
		Kind:        string(order.Kind),
		Label:       orderKindLabel(order.Kind),
		RemoveURL:   stanzaPath(entityID, order.Seq),
		RemoveValue: fmt.Sprintf("%d.%d", entityID, order.Seq),
	}
	if feedback.entity == entityID && feedback.seq == order.Seq {
		stanza.Error = feedback.message
	}
	// The boxes are the stored steps plus one blank on the end, which is the
	// box that appends. There is no other way to lengthen an order.
	for step := 1; step <= len(order.Steps)+1; step++ {
		box := orderBox{
			Step:  step,
			Name:  stepFieldName(entityID, order.Seq, step),
			Post:  fmt.Sprintf("%s/%d", stanzaPath(entityID, order.Seq), step),
			Label: fmt.Sprintf("%s step %d", orderKindLabel(order.Kind), step),
		}
		if step <= len(order.Steps) {
			box.Current = strings.ToLower(order.Steps[step-1].String())
		}
		stanza.Boxes = append(stanza.Boxes, box)
	}
	return stanza
}

// orderDirections are the six points a step box offers, in compass order. The
// blank option is in the template, because it is the absence of a direction
// rather than one of them.
func orderDirections() []orderDirection {
	points := compass.Points()
	directions := make([]orderDirection, 0, len(points))
	for _, point := range points {
		directions = append(directions, orderDirection{
			Value: strings.ToLower(point.String()),
			Label: fmt.Sprintf("%s %s", point, point.Name()),
		})
	}
	return directions
}

// orderKindLabel is how an order kind is written on the page. The stored form
// is the lowercase word; a heading is not.
func orderKindLabel(kind game.OrderKind) string {
	if kind == "" {
		return ""
	}
	return strings.ToUpper(string(kind[0])) + string(kind[1:])
}

func stanzaPath(entityID int64, seq int) string {
	return fmt.Sprintf("%s/%d/%d", ordersPath, entityID, seq)
}

func stepFieldName(entityID int64, seq, step int) string {
	return fmt.Sprintf("%s.%d.%d.%d", stepField, entityID, seq, step)
}

// pathStanza reads the entity and the stanza a request addresses out of its
// URL.
func pathStanza(r *http.Request) (int64, int, error) {
	entity, err := strconv.ParseInt(r.PathValue("entity"), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		return 0, 0, err
	}
	return entity, seq, nil
}

// parseStanzaAddress reads the "<entity>.<seq>" a remove button carries.
func parseStanzaAddress(value string) (int64, int, error) {
	entityText, seqText, found := strings.Cut(value, ".")
	if !found {
		return 0, 0, fmt.Errorf("order address %q: want <entity>.<seq>", value)
	}
	entity, err := strconv.ParseInt(entityText, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	seq, err := strconv.Atoi(seqText)
	if err != nil {
		return 0, 0, err
	}
	return entity, seq, nil
}

// parseDirection reads one step box's value. The blank option is the absence of
// a direction, which is the zero value the compass keeps invalid on purpose.
func parseDirection(value string) (compass.Point, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return compass.Parse(value)
}

// parseStepFields reads every step box a form carries into the stanzas they
// belong to.
//
// Blanks are dropped and the rest keep their relative order, which is what
// compacts a submitted page: "move nw <blank> e" arrives here as nw then e, and
// is stored as steps 1 and 2.
//
// The stanzas come back in a fixed order - by entity, then by sequence - so a
// save writes the same rows in the same order however a browser laid the form
// out.
func parseStepFields(form url.Values) ([]datastore.OrderSteps, error) {
	type box struct {
		step  int
		point compass.Point
	}
	// A stanza with every box blanked is still in the map, with no steps: a
	// save that dropped it would leave the order as it was rather than
	// clearing its last direction.
	boxes := make(map[[2]int64][]box)
	for name, values := range form {
		if !strings.HasPrefix(name, stepField+".") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(name, stepField+"."), ".")
		if len(parts) != 3 {
			return nil, fmt.Errorf("step field %q: want %s.<entity>.<seq>.<step>", name, stepField)
		}
		entity, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("step field %q: %w", name, err)
		}
		seq, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("step field %q: %w", name, err)
		}
		step, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("step field %q: %w", name, err)
		}
		point, err := parseDirection(values[0])
		if err != nil {
			return nil, fmt.Errorf("step field %q: %w", name, err)
		}
		key := [2]int64{entity, int64(seq)}
		if _, ok := boxes[key]; !ok {
			boxes[key] = nil
		}
		if point.IsValid() {
			boxes[key] = append(boxes[key], box{step: step, point: point})
		}
	}

	stanzas := make([]datastore.OrderSteps, 0, len(boxes))
	for key, filled := range boxes {
		sort.Slice(filled, func(i, j int) bool { return filled[i].step < filled[j].step })
		steps := make([]compass.Point, 0, len(filled))
		for _, box := range filled {
			steps = append(steps, box.point)
		}
		stanzas = append(stanzas, datastore.OrderSteps{EntityID: key[0], Seq: int(key[1]), Steps: steps})
	}
	sort.Slice(stanzas, func(i, j int) bool {
		if stanzas[i].EntityID != stanzas[j].EntityID {
			return stanzas[i].EntityID < stanzas[j].EntityID
		}
		return stanzas[i].Seq < stanzas[j].Seq
	})
	return stanzas, nil
}

// orderWriteFeedback turns a store's refusal into something a player can read,
// and into the status an unscripted request is answered with.
func orderWriteFeedback(err error, entity int64, seq int) orderFeedback {
	feedback := orderFeedback{entity: entity, seq: seq, status: http.StatusUnprocessableEntity}
	switch {
	case errors.Is(err, datastore.ErrTurnClosed):
		// The clock moved while the page was open. The page it comes back as
		// is the new turn's, so the message belongs to the page.
		return orderFeedback{
			message: "The turn advanced while this page was open. These are your orders for the new turn.",
			status:  http.StatusConflict,
		}
	case errors.Is(err, datastore.ErrOrderKindRefused):
		feedback.message = "That order is not one this can be given."
	case errors.Is(err, datastore.ErrUnknownOrder), errors.Is(err, datastore.ErrUnknownStep):
		feedback.message = "That order is no longer there."
	case errors.Is(err, datastore.ErrTooManySteps):
		feedback.message = datastore.ErrTooManySteps.Error() + "."
	case errors.Is(err, datastore.ErrUnknownEntity):
		return orderFeedback{
			message: "That is not one of your faction's.",
			status:  http.StatusNotFound,
		}
	default:
		feedback.message = "Marajanda could not save that order."
		feedback.status = http.StatusInternalServerError
	}
	return feedback
}
