# athena Roadmap

## Objective

Keep ATHENA moving through narrow physical-truth slices: read path first, then
publication, then real ingress, then persistence and prediction only when they
are earned.

## Current Line

Current active working line: `Phase 3 shared substrate A / v0.7.x` on `main`,
later than the `v0.6.1` Milestone 2.0 hardening follow-up.

`v0.5.0` remains the Tracer 16 durable-history release line. `v0.5.1` is the
bounded Tracer 17 support follow-up on that same durable-history branch.

- mock-backed occupancy read path is still real
- one source-backed CSV ingress adapter is now real locally
- identified arrival and departure publication is real
- explicit in-memory edge-driven occupancy projection is now real for
  `athena serve`
- one bounded internal HTTP facility-history read is now real on top of the
  existing durable-history groundwork to support HERMES reconciliation without
  widening into a public operator surface
- config-gated facility catalog, hours, zones, closure windows, and
  per-facility metadata reads are now real on `main` through CLI and internal
  HTTP over validated internal catalog files
- Postgres-backed append-only edge observations are now real in repo/runtime
- derived `open`, `closed`, and `unmatched_exit` session facts are now real in
  repo/runtime
- one bounded internal HTTP analytics read plus one CLI analytics read are now
  real in repo/runtime for facility, zone, node, and time-window questions
- bounded live in-cluster arrival proof is real through Milestone 1.5
- bounded live browser-reachable ATHENA edge ingress and occupancy proof is now
  real for `v0.4.1`
- the older local file-backed durable history path remains available only as an
  explicit fallback when Postgres is not configured
- bounded live proof still does not include the Postgres-backed durable branch,
  and broad ingress rollout is still unproven

## Current And Planned Release Lines

| Release line | Intended purpose | Restrictions | What it should not do yet |
| --- | --- | --- | --- |
| `v0.5.1` | bounded privacy-safe facility-history support follow-up on the existing durable-history line | keep the new route facility-filtered, read-only, and subordinate to durable-history truth | do not imply a public operator surface, identity-level reconciliation, or durable-branch deployment |
| `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads over validated internal catalog files | keep the read surfaces config-gated, internal/CLI, and subordinate to ATHENA-owned truth | do not widen into social logic or broad product UX |
| `v0.6.1` | Milestone 2.0 hardening follow-up for shutdown and publish resilience | keep the line patch-only and non-widening | do not claim durable-history deployment, Postgres ingress storage, or prediction |
| `v0.7.0` | Postgres-backed append-only observations, derived session facts, and bounded internal analytics reads | keep the new surfaces internal/CLI-first, keep fail-open durable writes explicit, and preserve ATHENA as the physical-truth ingest boundary | do not widen into booking, public dashboards, AI summaries, alias auto-merge, or prediction |
| later than `v0.7.0` | broader diagnostics and capacity prediction runtime | build on stable ingress, trusted durable history, derived sessions, and clean facility truth first | do not ship dashboards or predictive UX before prediction itself is real |

## Next Ladder Role

| Line | Role | Why it matters |
| --- | --- | --- |
| Tracer 17 support follow-up / `v0.5.1` | bounded privacy-safe facility-history support for HERMES reconciliation | lets HERMES consume durable history without private file access or broader ATHENA widening |
| `Tracer 18` / `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads | gives later sports, scheduling, and reporting logic trustworthy facility truth without activating writes, prediction, or shared contract widening |
| Milestone 2.0 hardening follow-up / `v0.6.1` | graceful shutdown plus bounded publish resilience on the existing runtime | keeps the current physical-truth line honest before any later diagnostics or prediction widening |
| Phase 3 shared substrate A / `v0.7.0` | Postgres-backed observations, derived sessions, and bounded internal analytics reads | gives manager-grade occupancy and flow work a real substrate before dashboards, scheduling, or AI summary copy |
| later than `v0.7.0` | broader diagnostics and capacity prediction runtime | keeps prediction later than trusted ingress, durable history, derived sessions, and facility truth |

## Verified Audit Carry-Forward

The `2026-04-13` backend logic audit reran `go test -count=1 ./...` and
re-read the projector, metrics, and read-path code before narrowing the next
ATHENA hardening work.

| Area | Ruling | Next honest line |
| --- | --- | --- |
| projector identity retention | verified medium: the in-process identity map is unbounded today | the first bounded hardening line later than `v0.7.0` should add an explicit retention, eviction, or cap strategy before dashboards, prediction, or AI summary work |
| metrics occupancy callback context | verified low: metrics still reads default occupancy through `context.Background()` | fold into the same bounded hardening patch if the metrics path is touched; do not pretend it is a new capability line |
| projector clock constructor | verified low readability cleanup: behavior is correct, but the default clock assignment is more confusing than it needs to be | only clean this up if the projector constructor is already open for the retention line |
| prediction / dashboards / AI summary / booking | unchanged and deferred | do not treat the audit as a reason to widen past the storage-and-analytics substrate that `v0.7.0` just closed |

## Boundaries

- Tracer 9 does not require ATHENA widening by default
- Tracer 10 keeps ingress physical-truth only and does not widen into
  deployment, prediction, or social logic
- the edge-driven occupancy slice still keeps projection in memory by default;
  the new Postgres-backed durable store is explicit, and the older file-backed
  history path remains only as a fallback
- the bounded live deployment uses one facility, one node token, a private GHCR
  pull secret, and a narrow HTTPS proxy path; it is not yet a broad ATHENA
  ingress rollout
- observed TouchNet pass/fail history is a plausible next ATHENA persistence
  slice, but operator override workflows should remain separate from the
  physical-truth ingest boundary
- the new bounded history read must stay privacy-safe, facility-filtered, and
  read-only; it exists to support HERMES rather than to turn ATHENA into a
  broad operator product surface
- duration-of-stay, tap frequency, and workstation-quality metrics should be
  derived from durable append-only observation history and session facts, not
  from ad hoc log scraping
- keep physical truth separate from member intent and product logic
- do not activate Redis-backed hot counters before the basic read path needs them
- do not widen into predictive dashboards before prediction is real

## Tracer / Workstream Ownership

- `Tracer 1`: first mock-backed read line
- `Tracer 2`: identified arrival publication
- `Tracer 5`: identified departure publication
- `Tracer 10`: first real ingress adapter
- separate ATHENA ingress slice: edge-driven occupancy projection from the same
  normalized event stream used for identified publish
- bounded deployment workstream: live browser-reachable HTTPS ingress for that
  same `v0.4.1` edge path
- that bounded deployment workstream is real deployment truth, but it did not
  consume a tracer number and should not be treated as partial `Tracer 16`
- `Milestone 1.6`: live departure-close proof in-cluster
- `Tracer 18`: facility metadata and hours after durable history is stable
- `Phase 3 shared substrate A`: Postgres-backed observations, derived sessions,
  and bounded internal analytics after the Phase 2 plateau stayed clean
- later line: broader diagnostics and prediction after source-backed ingress,
  durable sessions, and facility truth are stable
