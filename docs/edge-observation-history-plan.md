# Edge Observation History Plan

## Purpose

This document now records the layered ATHENA storage model after the closed
`v0.7.x` storage/analytics line and the current `v0.8.x`
policy-backed-admission testing line.

The planning question is no longer just:

`How do we store edge taps durably?`

It is now:

`How does ATHENA preserve immutable source observation truth while also representing explicit identity linkage, explicit admission policy, explicit accepted presence, and later derived session truth?`

## Current Truth

ATHENA already does all of the following in repo/runtime:

- accepts live TouchNet-shaped edge taps through `POST /api/v1/edge/tap`
- authenticates per-node tokens
- stores append-only `pass` and `fail` observations in Postgres when
  `ATHENA_EDGE_POSTGRES_DSN` is set
- normalizes fail observations to `bad_account_number`,
  `recognized_denied`, or `unclassified_fail`
- keeps source observation truth immutable even when a policy-backed admission
  decision exists later for the same tap
- maintains facility-local identity subjects and privacy-safe identity links
- enforces privacy-safe link-key shapes instead of trusting operator
  discipline
- maintains explicit access policy versions with actor attribution
- gives subject-scoped policies precedence over facility windows and rejects
  overlapping active policies in the same scope
- maintains explicit accepted-presence records separate from source observation
  rows
- replays occupancy from accepted-presence truth on restart
- keeps current `edge_sessions` as a separate derived layer over accepted
  source-pass observations only
- keeps the older file-backed history path only as an explicit fallback when
  Postgres is not configured

Bounded deployed truth is intentionally narrower than every possible code path:

- the bounded live deployment proved migration, rollout, replay, health, policy
  creation/readback, runtime wiring, and the conservative unattached-subject
  cleanup migration for `v0.8.2`
- it intentionally did not inject a synthetic production `recognized_denied`
  tap because durable production history is immutable
- `v0.8.2` hardens identity-link validation, first-seen subject creation,
  policy overlap rejection, and orphan cleanup

## Layered Truth Model

### 1. Immutable source observation truth

`athena.edge_observations` is the append-only record of what TouchNet showed.

Key properties:

- one row per observed tap
- source `result` remains `pass` or `fail`
- `failure_reason_code` is normalized
- raw account values, resolved names, and free-text `status_message` stay out
  of durable storage
- old rows are not rewritten to fit later operator interpretation

This is the physical-source fact layer.

### 2. Explicit identity-linkage truth

`athena.edge_identity_subjects` and `athena.edge_identity_links` represent
facility-local subject identity without auto-merging by name.

Key properties:

- one subject is scoped to one facility
- the first automatic link is `external_identity_hash`
- later links such as `member_account` or `qr_identity` are explicit,
  privacy-safe additions
- no name-based auto-merge exists in this model

This is the linkage layer, not the observation layer.

### 3. Explicit policy truth

`athena.edge_access_policies` and `athena.edge_access_policy_versions` record
who authorized admission logic and when.

Current `v0.8.x` policy modes:

- subject `always_admit`
- subject `grace_until`
- facility `facility_window` with
  `target_selector='recognized_denied_only'`

Current `v0.8.x` reason codes:

- `testing_rollout`
- `alumni_exception`
- `semester_rollover`
- `owner_exception`

Current `v0.8.x` actor attribution:

- actor kinds: `owner_user`, `service_account`, `system`
- surfaces: `athena_cli`, `migration_seed`, `future_admin_http`

This is the explicit policy layer.

Policy precedence in `v0.8.x` is explicit:

- subject-scoped policies beat facility-wide testing windows
- overlapping enabled subject policies for the same subject are rejected on
  create
- overlapping enabled facility windows for the same facility are rejected on
  create
- disabled policies remain historical versions, not deleted facts

### 4. Explicit accepted-presence truth

`athena.edge_presence_acceptances` records when ATHENA treated an observation as
accepted physical presence.

Current acceptance paths:

- `touchnet_pass`
- `always_admit`
- `grace_until`
- `facility_window`

Important rule:

- accepted presence is not a rewrite of source observation truth

That means a recognized denied tap can remain:

- source result: `fail`

while also becoming:

- accepted presence: `true`
- acceptance path: `facility_window`
- accepted reason code: `testing_rollout`

This is the accepted-presence layer.

### 5. Derived session truth

`athena.edge_sessions` still exists as a derived session layer.

Current `v0.8.x` rule:

- session derivation remains source-pass-only

That is deliberate. `v0.8.x` moves occupancy and replay onto accepted-presence
truth, but it does not yet claim stay-duration truth for policy-backed accepted
fails.

This is the staged-derived layer.

## Current Testing-Policy Boundary

The first-class testing policy in `v0.8.x` is facility-wide and explicit:

- `ATHENA_EDGE_POLICY_ACCEPTANCE_ENABLED=true`
- an active facility policy window exists
- target selector is `recognized_denied_only`

Recognition rules in this line:

- `recognized_denied` means source `result='fail'` and `name_present=true`
- `bad_account_number` never becomes accepted presence in this line
- fully up-to-date members with source `pass` are unaffected by the testing
  policy

This gives ATHENA a truthful way to say:

- TouchNet denied this tap
- the facility testing policy still treated it as accepted presence

without lying that TouchNet passed it.

## Current Internal Interfaces

The first policy and identity surface is intentionally internal and CLI-only:

- `athena policy create-facility-window`
- `athena policy create-subject`
- `athena policy disable`
- `athena policy list`
- `athena identity subject show`
- `athena identity link add`

There is still no HTTP admin surface in this line.

The bounded read surfaces now expose separated truth:

- source result remains visible
- accepted status is separate
- acceptance path is explicit
- accepted reason code is explicit

## What This Solves

This layered model solves the main truth problems that showed up during live
TouchNet rollout:

- source deny cannot be silently rewritten into source pass
- recognized denied taps can still participate in explicit testing-mode
  accepted presence
- gibberish input stays out of the admitted path
- occupancy and restart replay can follow accepted-presence truth instead of
  only source-pass truth
- identity linkage can grow without auto-merging by name
- later ATHENA or HERMES tooling can reason about policy-backed acceptance
  without flattening multiple truths into one

## What Is Still Deferred

The following are still deferred on purpose:

- accepted-presence session cutover
- policy-backed stay-duration truth
- HTTP admin surfaces for policy or identity management
- public report/export surfaces
- alias-management UX
- name-based auto-merge
- prediction and public dashboards

Those later lines should build on the current layered truth model instead of
collapsing it.
