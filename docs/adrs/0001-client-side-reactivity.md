# ADR 0001: Choose a client-side reactivity strategy

- Status: Proposed
- Date: 2026-09-11
- Issues: [#26](https://github.com/mdhender/marajanda/issues/26), [#67](https://github.com/mdhender/marajanda/issues/67), [#68](https://github.com/mdhender/marajanda/issues/68)

## Context

Users are asking for a more reactive application. That phrase does not yet
identify a technical requirement. It could mean faster feedback after a server
write, a disclosure or filter whose state belongs only to the browser, or a
continuous interaction such as drag or zoom. Those needs have different
solutions.

Marajanda currently uses server-rendered HTML and HTMX 2.0.10. The server owns
game and account state, and HTMX replaces complete regions with fragments. The
map and orders flows retain ordinary links and forms as fallbacks. This has
allowed previous work to avoid a reactive client runtime:

- map navigation uses native scrolling and server-rendered windows rather than
  client-managed drag and zoom;
- order controls save on change and replace the whole orders region rather than
  maintaining a second client-side order model; and
- the world-image warning is a page rather than a script-driven modal.

These are not merely missing enhancements. They are examples of changing the
interaction so that browser behavior or the server remains the source of
truth. That can continue to be the right answer if it meets the user's need.

Every rendered page and fragment currently sends this Content Security Policy:

```text
default-src 'self'; script-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'
```

The absence of `unsafe-eval` prevents JavaScript's `eval()` and `Function`
constructor. HTMX 2's `hx-on`, trigger filters, and `js:` forms of `hx-vals`
and `hx-headers` use those facilities. HTMX exposes `allowEval`, which defaults
to `true`, but the browser policy blocks the evaluation today. The current
application uses none of those attributes.

Issue #26 assumed that Alpine's CSP build forbids inline expressions and
requires every component to be registered in JavaScript. Alpine's current CSP
build has changed that trade-off: its documentation shows support for object
and array literals, common operators, assignments, updates, method calls, and
`x-model`. It still excludes advanced syntax, access to global variables and
functions, and HTML injection. Complex behavior is still better extracted into
registered `Alpine.data()` components. The standard Alpine build continues to
use the `Function` constructor and therefore requires `unsafe-eval`.

HTMX 4.0.0 is now stable. Its core remains a hypermedia library; upgrading does
not by itself add local reactivity. HTMX 4 offers `hx-live` as an optional,
DOM-oriented reactive extension and `hx-csp` for nonce-gated HTMX attributes,
Trusted Types, and expression evaluation without `unsafe-eval`. It is also a
major migration with changed inheritance, history, request, and error-response
semantics.

## Decision drivers

The selected end state should:

- solve demonstrated user interactions rather than add a runtime in
  anticipation of them;
- use native HTML and CSS before script, and server-rendered fragments before
  duplicating server-owned state in the browser;
- preserve ordinary link and form fallbacks for game capabilities where that
  remains practical;
- keep game rules, authorization, validation, and durable state on the server;
- preserve the current CSP unless a separately justified security decision
  changes it;
- behave predictably when HTMX replaces a region or restores history;
- remain accessible by keyboard and assistive technology; and
- fit the existing self-hosted, embedded-asset architecture without requiring
  a JavaScript build system unless its value justifies that change.

## Candidate end states

### 1. HTMX 2 only

Keep HTMX 2.0.10 and continue to use native controls, CSS, browser behavior,
and server round trips. A small same-origin JavaScript module could listen to
HTMX or DOM events for an isolated behavior, but this end state would not add a
reactive runtime or use evaluated attributes. If retained, the application
should set `htmx.config.allowEval = false` explicitly so its configuration says
what the CSP already enforces.

This is the smallest end state. There is one client dependency, no reactive
state to reconcile with server fragments, and no major-version migration.
Server-rendered responses keep behavior, validation, and presentation together.
It also preserves the successful pattern of simplifying an interaction before
adding client machinery.

Its limit is browser-local state. A server request is wasteful and can feel
slow for filtering markup already on the page, coordinating several controls,
previewing input, or maintaining a transient selection. Vanilla JavaScript can
cover an isolated case, but repeated hand-built state and binding code would be
evidence that this end state is no longer the simpler one. Staying on version 2
also postpones, rather than removes, the eventual major-version migration.

### 2. HTMX 2 with Alpine

Keep HTMX 2 for server communication and add Alpine for bounded islands of
browser-owned state. Under the current policy, the candidate is Alpine's CSP
build, not the standard build with `unsafe-eval`. Alpine would own such things
as a local disclosure, filter, preview, or coordinated input state; HTMX and
the server would continue to own requests and authoritative results.

This provides a mature reactive model with `x-data`, `x-on`, `x-show`,
`x-model`, stores, references, and watchers without paying the HTMX 4 migration
cost first. The current CSP build can express substantially more inline logic
than issue #26 originally recorded.

The cost is a second framework and a second lifecycle. Marajanda currently
uses `outerHTML` swaps for complete map and orders regions. Alpine state inside
one of those regions disappears when HTMX replaces it unless the state lives
outside the swap, the element is preserved, or the response deliberately
restores it. HTMX 2 history snapshots can also capture client-mutated DOM, and
content introduced by Alpine may need explicit HTMX processing. These
boundaries need design and browser tests for every island. The CSP build's
restricted expression language also means examples written for standard Alpine
cannot be copied without review.

Adding `unsafe-eval` to use standard Alpine is a variant of this end state, but
it is not the default candidate. The exception applies to every script allowed
by `script-src`, not only Alpine, and would also make injected HTMX expression
attributes executable. Convenience alone is not enough to weaken that layer.

### 3. HTMX 4

Upgrade to HTMX 4 and remain within its ecosystem. Core HTMX would continue to
own server requests and swaps. If the representative user need requires local
reactivity, add only the `hx-live` extension rather than the larger `htmax.js`
bundle. `hx-live` stores shared state in DOM attributes, binds attributes and
text to expressions, provides event handlers and DOM-query helpers, and
coordinates recomputation with HTMX swaps.

This end state keeps one interaction vocabulary and makes ARIA and `data-*`
attributes visible sources of UI state. HTMX 4 also provides explicit Alpine
compatibility if Alpine is adopted later, but that is not part of this end
state. The upgrade brings independent changes that may be valuable: explicit
attribute inheritance, `fetch()`, server-refetched history, status-aware swaps,
and morphing swaps.

The cost is the migration in #68 plus a comparatively new reactive extension.
Marajanda relies on HTMX 2's implicit inheritance in its map and orders regions,
and must deliberately adapt history and error responses. `hx-live` evaluates
expressions and runs every live expression after relevant DOM or input changes;
the team must test its CSP configuration and performance against real pages.
Non-morphing region replacement still destroys local state unless that state
lives above the swap boundary.

Keeping `unsafe-eval` out while using evaluated HTMX features requires
evaluating `hx-csp` with `safeEval:true`. That is not a free switch. The server
must create per-response nonces, add the nonce to the CSP and script elements,
stamp every trusted HTMX element with `hx-nonce`, issue fresh nonces for
fragments, and separate cached full and partial responses with
`Vary: HX-Request-Type`. The extension can then reject untrusted HTMX
attributes and enforce a Trusted Types policy, but user-controlled data must
still never be interpolated into an evaluated attribute.

## Comparison

| Concern | HTMX 2 | HTMX 2 + Alpine CSP | HTMX 4 with optional `hx-live` |
| --- | --- | --- | --- |
| Best fit | Server-owned state and isolated enhancements | Several browser-state islands | Server interactions plus DOM-oriented local reactivity |
| New runtime | None | Alpine CSP build | HTMX 4; `hx-live` if needed |
| CSP | Current policy; evaluated HTMX features unavailable | Current policy; Alpine CSP expression subset | Current policy without expressions; `hx-csp` and nonces for safe expression evaluation |
| State across current `outerHTML` swaps | Server redraws it | Must be placed or restored deliberately | Must be placed or restored deliberately |
| Immediate migration cost | Low | Medium | High |
| Ongoing conceptual cost | Low until custom scripts accumulate | Two frameworks and lifecycle boundary | One ecosystem, but extensions and newer APIs |
| Main risk | Round trips or bespoke JavaScript stop meeting UX needs | Client state conflicts with swaps/history | Major migration is undertaken without proving `hx-live` solves the need |

## Questions to answer before selecting an end state

### What are users actually unable to do comfortably?

1. Which requested interactions are concrete enough to reproduce and observe?
2. Is each interaction waiting on server-owned data, manipulating data already
   present in the page, or maintaining temporary state that the server should
   never see?
3. Is the problem latency, loss of focus or context after a swap, lack of
   immediate feedback, discoverability, animation, or continuous input?
4. Can native HTML, CSS, scrolling, validation, `details`, or a different page
   design meet the need before any JavaScript state is added?
5. Which one interaction is representative enough to implement as a spike in
   all viable approaches? A deliberately selected case is better evidence than
   comparing framework demonstrations.

### Where should state live?

1. What is the authoritative state, and what is only a browser projection or
   draft?
2. May local state reset after a successful request, a conflict, browser
   history navigation, or replacement of its containing region?
3. If it must survive, should it move outside the swap boundary, be encoded in
   a URL or form control, be returned by the server, or be preserved by a
   morphing mechanism?
4. How will two tabs or an API client changing the same game state be
   reconciled? A reactive client must not obscure the server's conflict result.
5. At what point would several isolated behaviors become a client-side model
   that duplicates game or validation rules?

### What progressive enhancement and accessibility contract is required?

1. Which capabilities must remain usable when script fails or is disabled, and
   which enhancements may disappear while the underlying task remains?
2. What should happen to focus, announcements, pending indicators, and typed
   values after both successful and refused swaps?
3. Can the candidate interaction use semantic controls and ARIA state rather
   than creating a custom widget?
4. Are keyboard, touch, screen-reader, reduced-motion, slow-network, and
   interrupted-request states included in the acceptance test?

### What security boundary will the team maintain?

1. Is `unsafe-eval` prohibited as an architectural rule? If not, what concrete
   user value outweighs weakening the policy for every permitted script?
2. Will evaluated attributes be allowed at all, and how will review prevent
   user-controlled values from entering them?
3. For Alpine, does its CSP expression subset cover the representative
   interaction, or does the code become clearer as a registered component?
4. For HTMX 4, is the team willing to implement and test per-response nonces,
   `hx-nonce` stamping, Trusted Types, fragment nonce rewriting, and cache
   variation before enabling `safeEval`?
5. Should HTMX 2 explicitly disable evaluation and script-tag processing while
   it remains in use?

### What maintenance cost is justified?

1. How many distinct reactive islands are expected in the next two product
   increments? One small interaction and a recurring interaction pattern imply
   different choices.
2. Is logic in the existing Go template string still readable when expressed
   as Alpine or `hx-live` attributes, or should non-trivial behavior live in a
   vendored application script?
3. Can the selected approach remain self-hosted and build-free, and is that
   constraint still valuable if application JavaScript grows?
4. Which approach gives tests that assert user-visible behavior rather than
   framework implementation details?
5. Who will maintain two-library integration if Alpine is selected, or the
   nonce and extension machinery if HTMX 4 with `hx-csp` is selected?

### Is the HTMX 4 upgrade independently warranted?

1. Would the team choose HTMX 4 for its supported lifecycle, history model,
   response handling, security extensions, or morphing even if no reactive
   feature were requested?
2. Does `hx-live` satisfy the representative case as clearly as Alpine's CSP
   build, including swap, history, and accessibility behavior?
3. Is adopting a newly released major version and a new extension preferable
   to first proving the product interaction on the current stable integration?
4. Can #68 be completed and verified independently, so migration defects are
   not confused with a new interaction's defects?

## Decision rule

No runtime should be selected from feature lists alone. First classify and
prototype one representative unmet interaction, with the same server contract
and acceptance tests for each viable implementation.

- Select **HTMX 2** if native behavior or a server-rendered interaction meets
  the need and isolated JavaScript remains exceptional.
- Select **HTMX 2 with Alpine CSP** if several demonstrated interactions need
  browser-owned reactive state and Alpine materially simplifies them without
  weakening CSP or duplicating server rules.
- Select **HTMX 4** when the major upgrade is justified on its own and
  `hx-live` proves sufficient for the same demonstrated interactions under an
  acceptable strict-CSP design.

Do not add Alpine for a single interaction that can be simplified, and do not
upgrade to HTMX 4 on the assumption that a newer HTMX core is inherently more
reactive. Until the questions above have evidence-backed answers, the current
HTMX 2 architecture remains the baseline rather than an implicitly rejected
option.

## References

- [HTMX 2 documentation: scripting and security](https://htmx.org/docs/#security)
- [HTMX 4 documentation](https://four.htmx.org/docs)
- [What's new in HTMX 4](https://four.htmx.org/docs/whats-new-in-htmx-4)
- [HTMX 4 `hx-live` extension](https://four.htmx.org/extensions/hx-live)
- [HTMX 4 `hx-csp` extension](https://four.htmx.org/extensions/hx-csp)
- [HTMX 4 Alpine compatibility extension](https://four.htmx.org/extensions/hx-alpine-compat)
- [Alpine CSP build](https://alpinejs.dev/advanced/csp)
