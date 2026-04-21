# athena Roadmap

## Objective

Keep ATHENA moving through narrow physical-truth slices: read path first, then
publication, then real ingress, then persistence and prediction only when they
are earned.

## Current Line

Current active working line: `v0.8.x` on `main`, later than the closed
`v0.7.x` storage/analytics line and the earlier `v0.6.1` Milestone 2.0
hardening follow-up.

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
- failure-reason normalization, facility-local identity subjects/links,
  explicit policy versions, and accepted-presence records are now real in
  repo/runtime
- `v0.8.2` hardening enforces privacy-safe identity-link keys, serializes
  first-seen subject creation, rejects overlapping active policies in the same
  scope, and codifies subject-policy precedence over facility windows
- one bounded internal HTTP analytics read plus one CLI analytics read are now
  real in repo/runtime for facility, zone, node, and time-window questions
- owner CLI policy and identity commands are now real in repo/runtime
- bounded live in-cluster arrival proof is real through Milestone 1.5
- bounded live browser-reachable ATHENA edge ingress and occupancy proof is now
  real for the deployed `v0.7.0` line
- the older local file-backed durable history path remains available only as an
  explicit fallback when Postgres is not configured
- bounded deployed truth for `v0.8.1` proves migration, rollout, replay,
  health, policy creation/readback, and runtime wiring; it intentionally did
  not inject a synthetic production `recognized_denied` tap

## Current And Planned Release Lines

| Release line | Intended purpose | Restrictions | What it should not do yet |
| --- | --- | --- | --- |
| `v0.5.1` | bounded privacy-safe facility-history support follow-up on the existing durable-history line | keep the new route facility-filtered, read-only, and subordinate to durable-history truth | do not imply a public operator surface, identity-level reconciliation, or durable-branch deployment |
| `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads over validated internal catalog files | keep the read surfaces config-gated, internal/CLI, and subordinate to ATHENA-owned truth | do not widen into social logic or broad product UX |
| `v0.6.1` | Milestone 2.0 hardening follow-up for shutdown and publish resilience | keep the line patch-only and non-widening | do not claim durable-history deployment, Postgres ingress storage, or prediction |
| `v0.7.0` | Postgres-backed append-only observations, derived session facts, and bounded internal analytics reads | keep the new surfaces internal/CLI-first, keep fail-open durable writes explicit, and preserve ATHENA as the physical-truth ingest boundary | do not widen into booking, public dashboards, AI summaries, alias auto-merge, or prediction |
| `v0.8.x` | policy-backed accepted-presence testing line | keep source `fail` truth immutable, require explicit policy versions, enforce privacy-safe links, keep policy/identity management internal CLI-only, and keep the existing tap contract unchanged | do not widen into session cutover, operator UI, public reports, alias UX, or prediction |
| later than `v0.8.x` | accepted-presence session cutover, broader diagnostics, and capacity prediction runtime | build on stable ingress, trusted durable history, explicit accepted presence, and clean facility truth first | do not ship dashboards, public reports, or predictive UX before the accepted-presence truth model is stable |

## Next Ladder Role

| Line | Role | Why it matters |
| --- | --- | --- |
| Tracer 17 support follow-up / `v0.5.1` | bounded privacy-safe facility-history support for HERMES reconciliation | lets HERMES consume durable history without private file access or broader ATHENA widening |
| `Tracer 18` / `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads | gives later sports, scheduling, and reporting logic trustworthy facility truth without activating writes, prediction, or shared contract widening |
| Milestone 2.0 hardening follow-up / `v0.6.1` | graceful shutdown plus bounded publish resilience on the existing runtime | keeps the current physical-truth line honest before any later diagnostics or prediction widening |
| Phase 3 shared substrate A / `v0.7.0` | Postgres-backed observations, derived sessions, and bounded internal analytics reads | gives manager-grade occupancy and flow work a real substrate before dashboards, scheduling, or AI summary copy |
| current `v0.8.x` line | policy-backed accepted presence for explicit recognized-denied testing windows | lets ATHENA represent local testing admission honestly without rewriting TouchNet source truth or widening into a broader operator product surface |
| later than `v0.8.x` | accepted-presence session cutover, broader diagnostics, and capacity prediction runtime | keeps duration/reporting/prediction later than trusted ingress, durable history, accepted presence, and facility truth |

## Verified Audit Carry-Forward

The `2026-04-13` backend logic audit reran `go test -count=1 ./...` and
re-read the projector, metrics, and read-path code before narrowing the next
ATHENA hardening work.

| Area | Ruling | Next honest line |
| --- | --- | --- |
| projector identity retention | verified bounded: the in-process identity map now keeps absent identities only within retention and cap limits | keep the destructive coverage on age, cap, order, replay, and fresh re-entry boundaries honest, and state clearly that stale/duplicate protection for evicted absent identities is intentionally bounded while replay remains authoritative |
| durable projector miss guardrail | verified bounded: committed pass writes now maintain compact durable identity markers, and projector misses consult them before accepting an older or duplicate event | keep replay authoritative, keep the file fallback compatible, and keep source/site ordering contract redesign deferred instead of sneaking it into this patch |
| metrics occupancy callback context | verified bounded: metrics now reads default occupancy through a context-free snapshot helper | verify the server path still matches the read path; do not widen observability scope |
| projector clock constructor | verified bounded readability cleanup: the constructor now defaults cleanly while still allowing an explicit clock | keep alignment checks narrow and patch-only |
| prediction / dashboards / AI summary / booking | unchanged and deferred | do not treat the audit as a reason to widen past the storage-and-analytics substrate that `v0.7.0` just closed |

## Boundaries

- Tracer 9 does not require ATHENA widening by default
- Tracer 10 keeps ingress physical-truth only and does not widen into
  deployment, prediction, or social logic
- the edge-driven occupancy slice still keeps projection in memory by default;
  the new Postgres-backed durable store is explicit, and the older file-backed
  history path remains only as a fallback
- compact durable identity markers now guard projector misses after absent-state
  eviction, but they do not replace replay as the occupancy authority
- source `fail` truth must stay immutable even when a policy-backed acceptance
  exists for the same observation
- accepted presence can widen occupancy and replay truth now, but session
  derivation stays source-pass-only in `v0.8.x`
- policy and identity management stay internal CLI-only in this line; there is
  still no HTTP admin surface
- the bounded live deployment uses one facility, one node token, a private GHCR
  pull secret, and a narrow HTTPS proxy path; it is not yet a broad ATHENA
  ingress rollout
- operator override or alias-management workflows should remain separate from
  the physical-truth ingest boundary
- the new bounded history read must stay privacy-safe, facility-filtered, and
  read-only; it exists to support HERMES rather than to turn ATHENA into a
  broad operator product surface
- duration-of-stay, tap frequency, and workstation-quality metrics should be
  derived from durable append-only observation history and session facts, not
  from ad hoc log scraping
- source/site ordering contract redesign remains deferred to a later ingest
  redesign line; this patch closes the projector-miss gap without widening that
  contract
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
- current line: `v0.8.x` policy-backed accepted presence after the storage line
  stayed clean
- later line: accepted-presence session cutover, broader diagnostics, and
  prediction after source-backed ingress, durable sessions, accepted presence,
  and facility truth are stable
