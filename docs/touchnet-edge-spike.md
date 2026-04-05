# TouchNet Edge Spike

## Purpose

This document records the narrow TouchNet browser-bridge spike that was used to
prove live edge ingress into `athena`.

The goal of the spike was not to replace TouchNet policy or staff judgment. The
goal was to answer one bounded question:

`Can ATHENA observe TouchNet access attempts in real time, preserve enough operator context to reconcile identities later, and still publish only the safe canonical visit-lifecycle events downstream?`

The answer is yes.

## What Was Proven

The following flow is now locally proven against the real TouchNet Verify
Account Entry page shape:

`TouchNet page -> Tampermonkey userscript -> POST /api/v1/edge/tap -> ATHENA -> NATS -> APOLLO`

What ATHENA now receives from the browser bridge:

- `account_raw`
- `account_type`
- `name`
- `status_message`
- `result` as `pass` or `fail`
- inferred `direction` as `in` or `out`
- `observed_at`
- `node_id`

What ATHENA does with that data today:

- logs both `pass` and `fail` observations with full operator context
- hashes `account_raw` immediately for the canonical publishable identity
- publishes only `pass` observations as identified arrival or departure events
- keeps `fail` observations local to ATHENA logs for diagnostics and future
  operator workflows

## Current Boundary

ATHENA is the ingestion and normalization boundary.

ATHENA currently does **not** decide:

- whether a denied student should still be admitted manually
- whether a specific person has a standing exception
- whether staff overrode a bad access rule correctly

Those are policy and operator-workflow questions, not physical-truth ingestion
questions.

Current boundary split:

- TouchNet decides `pass` or `fail`
- ATHENA observes that decision and publishes only the canonical safe lifecycle
  events
- a future admin/staff surface can review failures, correlate identities, and
  record manual overrides or exceptions

## Identity Model

TouchNet can surface multiple identifiers for the same person:

- student number, often through `Standard`
- RFID or card-derived number, often through `ISO`
- resolved human name

That means the same person may appear under different raw account values
depending on what they presented at the reader.

The current spike handles that carefully:

- the raw account value is preserved in ATHENA logs
- the published downstream identity is the hash of the raw account value
- the operator context includes `account_type` and `name`

That is enough to reconcile student number and RFID later, but it is not yet a
true identity-linking system.

To move from "observable" to "reconciled", the platform would need an explicit
mapping layer, for example:

- TouchNet or another source system becomes the source of truth for
  student-number-to-card mappings
- or an admin workflow records confirmed aliases for the same person

That mapping should not be invented by the ATHENA browser bridge.

## Denied But Admitted

The important operational case is:

`TouchNet says fail, staff let the student in anyway.`

That should not be modeled as "pretend the access pass happened."

The safer model is:

1. Keep the original TouchNet observation exactly as it happened.
2. Record a separate override or exception decision with staff attribution.
3. Make downstream occupancy or audit logic explicit about whether it is reading:
   - raw access truth
   - manual override truth
   - or a combined operational view

Recommended model:

- ATHENA stores the original TouchNet observation immutably
- HERMES exposes an admin-facing review and override surface
- a future write path emits a separate operator action such as:
  - `manual_admit`
  - `temporary_exception`
  - `identity_alias_confirmed`

That keeps the physical truth and the human override distinct.

## Lossless Storage Recommendation

If this spike graduates beyond log-only proof, the next good persistence slice
is an append-only edge observation table in ATHENA.

Recommended stored fields:

- `event_id`
- `observed_at`
- `facility_id`
- `zone_id`
- `node_id`
- `direction`
- `result`
- `account_raw`
- `account_type`
- `name`
- `status_message`
- `external_identity_hash`
- `raw_payload_json`
- `created_at`

Recommended rules:

- store the raw observation losslessly
- keep the hashed canonical identity alongside it
- never mutate the original observation row
- record overrides in a separate table with staff attribution, reason, and time

Recommended future override table shape:

- `override_id`
- `edge_event_id`
- `action`
- `operator_id`
- `reason`
- `notes`
- `created_at`

That gives auditability without corrupting the original TouchNet truth.

## Where Each Piece Belongs

### ATHENA

ATHENA should own:

- TouchNet edge ingestion
- account hashing
- immutable observed access-attempt history
- publishable visit-lifecycle event creation

ATHENA should not own:

- broad staff case management
- operator approvals UI
- identity reconciliation UX
- ad hoc policy exceptions as product logic

### HERMES

HERMES is the better future home for:

- admin or manager review screens
- search by student number, RFID, or name
- failure audit views
- manual override and exception actions
- staff-facing reconciliation workflows

### ashton-proto

If downstream consumers truly need more than the hashed identity, the shared
event contract would need to change in `ashton-proto`.

That should be a deliberate contract decision, not a hidden side effect of the
browser bridge.

### APOLLO

APOLLO should continue treating ATHENA as the source of physical-truth visit
lifecycle events. APOLLO should not need raw TouchNet PII unless the shared
contract is intentionally widened.

## Methodology Used In This Spike

The spike used a deliberately narrow process:

1. Prove ATHENA edge ingress locally with a bounded HTTP route.
2. Replay TouchNet-shaped CSV exports through the same route.
3. Build a Tampermonkey userscript against the real saved page selectors.
4. Confirm live browser posting against local ATHENA and local NATS.
5. Extend the browser bridge to include richer operator context.
6. Preserve safe downstream behavior by publishing only `pass` observations.

Validation used:

- unit tests for edge ingress normalization and handler behavior
- unit tests for raw TouchNet replay parsing
- end-to-end local smoke against a real running ATHENA server
- live browser tests against the actual TouchNet page shape

## Hurdles Encountered

- The first userscript assumed placeholder selectors instead of the real
  TouchNet DOM.
- The real page timestamps required custom parsing instead of a naïve
  `Date(...)` assumption.
- The browser bridge needed Tampermonkey cross-origin requests rather than
  plain in-page `fetch()` for a realistic Chrome path.
- Real operator value required observing both `pass` and `fail`, not just
  publishing the happy path.
- The canonical downstream event shape only carries the hashed identity, so
  operator context currently lives in ATHENA logs rather than a queryable API.

## Current Recommendation

Short term:

- keep this code in `ixxet/athena`
- commit the spike as ATHENA ingress work, not as a random side project
- keep HERMES notes in documentation only until a real admin read or write
  slice is started

Next likely slices:

1. ATHENA persistence for immutable edge observations
2. HERMES read-only admin query over observed TouchNet edge history
3. HERMES write path for manual admit or exception actions
4. optional `ashton-proto` changes only if downstream consumers truly need more
   identity context than the hash

## Suggested Commit Shape

This spike is large enough that it should land as several intentional commits
rather than one blob.

Recommended sequence:

1. `athena: add edge tap ingress and raw touchnet replay`
2. `athena: add touchnet browser bridge fixture and runbook`
3. `athena: observe touchnet fail rows and richer operator context`
4. `docs: add touchnet edge spike design note`

That keeps the runtime changes, browser fixture work, and design explanation
separable in review.
