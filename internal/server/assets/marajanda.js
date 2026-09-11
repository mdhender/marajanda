// Copyright (c) 2026 Michael D Henderson.

// The one job script does here that HTML and CSS cannot.
//
// A write to the orders page that loses a race is answered with the conflict
// notice alone: the list is left exactly as the player knew it, because the
// orders the other client wrote are not orders this player has ever seen. That
// is the right answer and it has a consequence - the response is not drawing
// the controls, so it cannot put a disabled attribute on them.
//
// CSS cannot stand in. `pointer-events: none` stops a mouse and nothing else:
// Tab still walks every control, Space and Enter still activate one, and an
// accessibility tree with no disabled state in it reports a live form. The page
// then tells a sighted mouse user the form is off and tells everybody else it
// is on. See issue #66.
//
// Nothing is lost by using script for it. The notice-alone swap only ever
// happens when HTMX is running; with the script blocked, a conflicting write is
// an ordinary page load that redraws the whole list, and there is nothing on
// screen to disable.
(function () {
    'use strict';

    // reconcileOrdersConflict points the fieldset's disabled state at the one
    // fact that decides it: whether a conflict notice is on the page.
    //
    // It is a reconciliation rather than a switch, so it is safe to run after
    // every swap and in any order. A write that lands replaces the whole
    // region, and the form that arrives is drawn enabled with no notice beside
    // it, so the same line that turns the controls off turns them back on.
    function reconcileOrdersConflict() {
        var orders = document.getElementById('orders');
        if (orders === null) {
            return;
        }
        var controls = orders.querySelector('#orders-controls');
        if (controls === null) {
            return;
        }
        controls.disabled = orders.querySelector('.orders-conflict') !== null;
    }

    // HTMX events bubble, so one listener on the document sees every swap the
    // page makes.
    document.addEventListener('htmx:afterSwap', reconcileOrdersConflict);

    // The script is deferred, so the document is parsed by the time this runs
    // and the first reconciliation is owed to the page as loaded.
    reconcileOrdersConflict();
})();
