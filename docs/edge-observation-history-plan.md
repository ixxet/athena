# Edge Observation History Plan

## Purpose

This document records the next ATHENA planning slice after bounded live
edge-driven occupancy deployment:

`How should ATHENA store observed edge history durably enough to support audit, flow analysis, frequency analysis, and inferred stay duration without widening product logic prematurely?`

The short answer is:

- keep ATHENA as the physical-truth ingest boundary
- persist raw edge observations append-only
- derive analytics and inferred sessions from those observations
- keep manual overrides and broader operator workflows out of the same slice

Phase 3 shared substrate A update:

- append-only Postgres-backed observation persistence is now real in
  repo/runtime
- derived `open`, `closed`, and `unmatched_exit` session facts are now real in
  repo/runtime
- compact durable identity markers are now real in repo/runtime for projector
  miss protection after absent-state eviction
- privacy-safe CLI/internal history reads plus one bounded internal analytics
  read are now real in repo/runtime
- replay of committed `pass` observations into a fresh projector is now real in
  repo/runtime from the configured durable store
- deployed truth still does **not** prove that the Postgres-backed durable
  branch live

The standalone Mermaid source for this planning flow lives at
[`docs/diagrams/edge-observation-history.mmd`](docs/diagrams/edge-observation-history.mmd).

## Current Truth

What ATHENA already does:

- accepts live TouchNet-shaped edge taps through `POST /api/v1/edge/tap`
- authenticates per-node tokens
- preserves `pass` and `fail` observations in an append-only Postgres-backed
  history path when configured
- updates in-memory live occupancy from accepted `pass` events in explicit
  projection mode
- publishes safe identified arrival/departure events downstream
- derives `open`, `closed`, and `unmatched_exit` session facts from accepted
  `pass` events
- writes a compact durable last-seen marker for each committed `pass` identity
  key and consults it on projector misses before accepting a supposedly fresh
  event
- exposes one bounded internal analytics read over facility, zone, node, and
  time window
- replays committed `pass` observations from the configured durable store into
  a fresh projector when durable history is configured
- keeps downstream payloads on the hashed identity, not the raw account value
- keeps the older file-backed history path only as an explicit local/runtime
  fallback when Postgres is not configured

What ATHENA does **not** do yet:

- prove the Postgres-backed durable-history line in deployed truth
- store occupancy snapshots durably
- expose query/search APIs over observed edge history
- reconcile student-number aliases and RFID aliases into a canonical person
- redesign the source/site ordering contract for ingest conflict resolution
- widen into booking, public dashboards, or AI summary surfaces

That means the next storage slice is not about inventing new runtime truth. It
is about preserving the truth ATHENA already sees.

The current marker line is intentionally narrow:

- markers are a compact guardrail for projector misses, not a second occupancy
  authority
- replay from committed `pass` observations remains authoritative
- source/site ordering contract redesign is explicitly deferred to a later
  ingest redesign instead of being smuggled into this patch

## Questions The Next Slice Should Answer

Durable edge history should make the following questions answerable:

- How many taps happened by facility, zone, workstation, hour, and day?
- How often do `pass` and `fail` attempts occur?
- Which failure reasons happen most often?
- Which readers generate the most malformed scans or mismatched account-type
  attempts?
- How many unique people visited a facility over a period?
- How long did a person likely stay, based on accepted `in` and `out` taps?
- How often do repeated `in`, repeated `out`, and `already_absent` /
  `already_present` cases happen?
- How often does the same person appear under different account types or raw
  account values?

These are ATHENA-appropriate operational and physical-truth questions. They are
not yet override workflow, staffing workflow, or member-intent questions.

## Recommended Storage Model

### 1. Append-only edge observations

The first durable slice should be one immutable table for every observed edge
attempt, including both `pass` and `fail`.

Current runtime fields:

- `observation_id`
- `event_id`
- `facility_id`
- `zone_id`
- `node_id`
- `direction`
- `result`
- `source`
- `observed_at`
- `stored_at`
- `account_type`
- `name_present`
- `external_identity_hash`
- `created_at`

Recommended rules:

- `event_id` stays the idempotency key
- the row is never updated after write
- both `pass` and `fail` are stored
- the hashed identity is always stored
- raw account values, resolved names, and free-text `status_message` stay out
  of the durable row and out of downstream publish payloads
- commit truth stays separate in an append-only `edge_observation_commits`
  table so accepted `pass` events remain explicit

### 2. Derived session facts, not handwritten occupancy history

The next thing to derive is not a mutable occupancy ledger. It is an inferred
session layer built from accepted `pass` events.

Current runtime shape:

- `session_id`
- `external_identity_hash`
- `facility_id`
- `zone_id`
- `entry_node_id`
- `entry_event_id`
- `entry_at`
- `exit_node_id`
- `exit_event_id`
- `exit_at`
- `duration_seconds`
- `session_state` as `open`, `closed`, or `unmatched_exit`
- `created_at`
- `updated_at`

Rules:

- derive sessions from accepted `pass` events only
- never rewrite the original observation history
- treat unmatched `out` and unmatched `in` as explicit analytic states, not as
  silent data loss

### 3. Alias mapping must be explicit

TouchNet can surface the same person under:

- `Standard` student number
- `ISO` / card-derived number
- resolved name

ATHENA should not auto-merge identities based only on name. The storage plan
should leave room for an explicit alias layer later.

Recommended future table:

- `alias_id`
- `canonical_person_id`
- `external_identity_hash`
- `account_type`
- `source`
- `confidence`
- `confirmed_by`
- `confirmed_at`
- `notes`

This lets ATHENA or a later admin surface record:

- trusted source-system mappings
- operator-confirmed aliases
- candidate links that still need confirmation

## Metrics And Analytics To Derive

Once append-only storage exists, ATHENA should be able to produce:

### Flow metrics

- taps per 15-minute bin by facility
- taps per 15-minute bin by zone
- taps per workstation / node
- `pass` versus `fail` counts over time
- entry versus exit ratios by window

### Quality metrics

- malformed account attempts
- `bad account number` frequency
- `no rule matches account` frequency
- repeated `already_present` and `already_absent` outcomes
- account-type mismatch frequency such as `Standard` input used against an RFID
  number or vice versa

### Visit metrics

- unique visitors per day
- repeat visitors per week
- inferred average stay duration
- median stay duration
- open sessions at cutoff time
- unmatched exits and unmatched entries

### Identity-reconciliation metrics

- same resolved name appearing under multiple `external_identity_hash` values
- same reader producing both `Standard` and `ISO` for likely related identities
- candidate alias counts by facility

## Recommended Implementation Order

Keep this narrow and honest:

1. Keep append-only observation persistence explicit, bounded, and privacy-safe.
2. Keep accepted-pass commit truth explicit instead of inferring it from all
   `pass` rows.
3. Add richer repo-internal CLI/read models over durable history first, not
   broad public HTTP report surfaces or operator UI.
4. Add derived session materialization for duration and visit analytics.
5. Add alias candidates and explicit alias confirmation later.

Do **not** start with:

- override workflows
- occupancy snapshot persistence
- broad dashboard UI
- automatic identity merging by name

## First Rollout Posture

The first durable rollout should preserve tomorrow's live tap collection path.

Start with:

- the same tunnel, token, and userscript contract
- persistence added behind the existing `POST /api/v1/edge/tap` handler
- fail-open shadow-write posture for the first rollout

That means:

- if the durable write fails, ATHENA should still accept the tap
- the current live occupancy and identified publish path should keep working
- degraded persistence must become explicit through logs and metrics

Revisit fail-closed behavior only after the durable path is trusted.

## Data Handling Rules

- Published downstream events remain hash-only.
- Raw account values stay inside ATHENA-owned restricted storage.
- Name and status fields are operationally useful, but still sensitive.
- Observation storage should support retention and purge rules from the start.
- Session analytics should be reproducible from append-only truth, not from
  mutable ad hoc edits.

## What Tonight's Live Proof Added

The bounded live workstation proof surfaced several real requirements for the
durable storage slice:

- workstation-neutral node IDs are still useful even when direction is inferred
  from the TouchNet row
- `pass`, `fail`, malformed input, and mismatched account-type attempts all
  happen in normal operator testing and should be stored losslessly
- repeated `in` / `out` attempts and `already_absent` / `already_present`
  outcomes are analytically meaningful, not just noise
- legacy browser environments may need compatibility-targeted bridge variants,
  which means node-level attribution matters when debugging ingestion quality

## Future Ownership Split

ATHENA should own:

- append-only edge observation history
- hashed identity handling
- derived session analytics
- source-quality and reader-quality metrics

Later services such as HERMES should own:

- operator search UI
- manual admit / override workflows
- staff-facing reconciliation UX
- exception approval and review

## Definition Of The Next Storage Slice

The next ATHENA storage slice is done when:

- live edge observations are durable and append-only
- pass/fail history is queryable internally
- a first inferred session layer can compute entry/exit duration safely
- identity aliasing remains explicit and does not silently widen downstream
  publish contracts
- docs still state honestly that overrides and broader staff workflows remain
  out of scope
