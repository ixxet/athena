# ATHENA Edge Ingress Runbook

## Purpose

Use this runbook to prove the new push-based ATHENA ingress slice:

`TouchNet-shaped source -> POST /api/v1/edge/tap -> in-memory occupancy projection + NATS -> APOLLO`

In explicit edge-projection mode, the same normalized pass event now drives:

- live occupancy projection for `/api/v1/presence/count` and `/metrics`
- identified arrival or departure publication to NATS

When `ATHENA_EDGE_OBSERVATION_HISTORY_PATH` is set, every authorized edge
observation is also shadow-written append-only after normalization and token
auth, before the existing `fail` / `pass` split. Durable-write failure stays
fail-open for the live ingress path.

## Environment

Start ATHENA with NATS plus edge-ingress config:

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena

ATHENA_HTTP_ADDR=127.0.0.1:18090 \
ATHENA_ADAPTER=mock \
ATHENA_NATS_URL=nats://127.0.0.1:4222 \
ATHENA_EDGE_HASH_SALT=demo-salt \
ATHENA_EDGE_TOKENS='entry-node=entry-token,exit-node=exit-token' \
ATHENA_EDGE_OCCUPANCY_PROJECTION=true \
ATHENA_EDGE_OBSERVATION_HISTORY_PATH=/tmp/athena-edge-history.jsonl \
go run ./cmd/athena serve
```

If the edge config is valid, `/api/v1/edge/tap` is mounted automatically and
`/api/v1/presence/count` now reads from the in-memory edge projection. If the
history path is configured, ATHENA replays committed `pass` observations before
it starts listening.

## Raw TouchNet Replay

The replay command accepts the raw report shape directly:

- `ACCOUNT`
- `NAME`
- `LOCATION`
- `DATE TIME`

Example:

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena

go run ./cmd/athena edge replay-touchnet \
  --csv-path /tmp/touchnet-access.csv \
  --facility ashtonbee \
  --zone gym-floor \
  --entry-location 'Entry Reader' \
  --exit-location 'Exit Reader' \
  --base-url http://127.0.0.1:18090 \
  --node-id entry-node \
  --token entry-token \
  --time-scale 0
```

Expected result:

- the command exits zero
- ATHENA logs `edge tap accepted`
- `GET /api/v1/presence/count?facility=ashtonbee&zone=gym-floor` reflects the
  replayed live projection state
- identified presence messages publish to the existing NATS subjects

## Internal Durable History Check

Use the internal-only CLI surface to inspect recent stored observations without
widening HTTP:

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena

go run ./cmd/athena edge history \
  --history-path /tmp/athena-edge-history.jsonl \
  --limit 20 \
  --format json
```

Expected result:

- the output contains `external_identity_hash`
- the output does not contain raw account numbers
- the output does not contain resolved names

Bounded internal HTTP history support for HERMES:

```bash
curl -sS 'http://127.0.0.1:18090/api/v1/presence/history?facility=ashtonbee&since=2026-04-09T11:00:00Z&until=2026-04-09T13:00:00Z'
```

Expected result:

- the output contains only `direction`, `result`, `observed_at`, and
  `committed` for each observation
- the output does not contain raw account numbers
- the output does not contain resolved names
- the output does not contain `external_identity_hash`

## Internal Ingress Bridge Proof

Use the repo/local CLI proof when a later APOLLO packet needs to know which
ATHENA facts are eligible for future co-presence, private daily presence, or
reliability verification work:

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena

go run ./cmd/athena edge ingress-bridge \
  --postgres-dsn "$ATHENA_EDGE_POSTGRES_DSN" \
  --facility ashtonbee \
  --zone gym-floor \
  --since 2026-04-09T11:00:00Z \
  --until 2026-04-09T13:00:00Z \
  --format json
```

Text output is also available:

```bash
go run ./cmd/athena edge ingress-bridge \
  --postgres-dsn "$ATHENA_EDGE_POSTGRES_DSN" \
  --facility ashtonbee \
  --since 2026-04-09T11:00:00Z \
  --until 2026-04-09T13:00:00Z \
  --format text
```

Expected result:

- source `pass` / `fail` remains explicit and immutable
- policy-backed accepted presence appears as separate accepted truth, not as a
  rewritten source `pass`
- source-pass `edge_sessions` remain separate from accepted-presence truth
- accepted-presence session cutover is not claimed
- anonymous or missing identity, unknown identity when a known set is supplied
  by a caller, source fail without acceptance, stale, duplicate, out-of-order,
  missing facility, missing timestamp, incomplete lifecycle, and
  accepted-presence-without-source-pass-session cases carry explicit reason
  codes
- JSON/text output redacts raw account IDs, names, and external identity hashes

This is an internal CLI proof over existing ATHENA Postgres facts. It does not
create APOLLO visits, XP, teams, reliability scores, public/member routes,
frontend UI, schema/proto changes, or deployed truth.

## Browser Fixture

Use these files before the facility reopens:

- `docs/runbooks/fixtures/touchnet-edge.user.js`
- `docs/runbooks/fixtures/touchnet-edge-fixture.html`
- `docs/runbooks/fixtures/touchnet-edge-template-modern.user.js`
- `docs/runbooks/fixtures/touchnet-edge-template-legacy.user.js`

Workflow:

1. Start ATHENA locally with edge ingress enabled.
2. Open the HTML fixture in a browser.
3. Install Tampermonkey in Chrome.
4. Create a new userscript, paste in `docs/runbooks/fixtures/touchnet-edge.user.js`, and update:
   - `baseUrl`
   - `nodeId`
   - `token`
   - `facilityId`
   - `zoneId`
5. If you test against the local HTML fixture, enable Tampermonkey access to file URLs.
6. Use the fixture buttons to append `Pass` or `Denied` rows.
7. Confirm that only `Pass` rows affect occupancy and publication, while
   repeated rerenders do not duplicate accepted events or inflate counts.

Template note:

- use `touchnet-edge-template-modern.user.js` for current Chrome/Windows
  workstations
- use `touchnet-edge-template-legacy.user.js` for older ChromeOS or legacy
  Tampermonkey environments
- keep real secret-bearing workstation copies local only so active node tokens
  do not get committed by accident
- the repo ignore rule for `touchnet-edge-live-*.user.js` keeps those local
  workstation variants out of Git as long as they stay untracked

Current bounded node registry:

| Facility | Zone | Node ID | Typical machine | Local live script |
| --- | --- | --- | --- | --- |
| `ashtonbee` | `gym-floor` | `ash-gym-01` | Windows / modern Tampermonkey | `touchnet-edge-live-ashtonbee-ash-gym-01.user.js` |
| `ashtonbee` | `gym-floor` | `ash-gym-02` | Chromebook / legacy Tampermonkey | `touchnet-edge-live-ashtonbee-ash-gym-02-legacy.user.js` |
| `morningside` | `weight-room` | `ms-gym-01` | Windows / modern Tampermonkey | `touchnet-edge-live-morningside-ms-gym-01.user.js` |
| `morningside` | `weight-room` | `ms-gym-02` | Chromebook / legacy Tampermonkey | `touchnet-edge-live-morningside-ms-gym-02-legacy.user.js` |

The node ID identifies the workstation only. It does **not** hardcode entry or
exit polarity. Direction still comes from the TouchNet row text so a reader can
physically swap roles later without renaming the node.

## Chrome Quick Start

For the real TouchNet page:

1. Open the Verify Account Entry page in Chrome.
2. Open Tampermonkey, enable the `Ashton TouchNet Edge Bridge` script, and save it.
3. Set the script values:
   - `baseUrl` to `https://tap.lintellabs.net` for the live cluster, or to your
     local ATHENA URL such as `http://127.0.0.1:18090` only when doing a local
     workstation smoke
   - `nodeId` to the node configured in `ATHENA_EDGE_TOKENS`
   - `token` to the token paired with that node
4. Refresh the TouchNet page once after saving the script.
5. Keep cursor focus in the account field. The scanner still depends on that
   focus, but the red warning banner is now disabled by default because it is
   visually noisy during normal operation.
6. Scan a card or type a number and press Enter.
7. Watch ATHENA logs for `edge tap accepted`.

The userscript posts through Tampermonkey's cross-origin request API, so it is
better suited to a real browser test than plain in-page `fetch()`.

## Workstation Runtime Notes

- the modern and legacy scripts now match only TouchNet-like hosts plus the
  local `file://` fixture, instead of running on every page
- routine success-path console logging is off by default; warnings now only
  surface on misconfiguration or failed delivery
- the red focus banner stays disabled unless `showFocusBanner` is turned on for
  troubleshooting
- if multiple workstation scripts are accidentally enabled in one browser
  profile, only the first one will activate on a page
- Chrome lag is more likely when:
  - multiple workstation scripts are enabled in the same profile
  - a very broad `@match` keeps the script active on unrelated pages
  - DevTools is left open on a noisy legacy script build

## Bounded Live Deployment Proof

`v0.4.1` is now bounded live deployment truth, not only local tracer truth.

What is live:

- ATHENA runs in-cluster with `ATHENA_EDGE_OCCUPANCY_PROJECTION=true`
- the cluster pulls the private ATHENA image through a dedicated GHCR image pull
  secret
- a narrow HTTPS path is exposed through a Cloudflare quick tunnel
- the public path is intentionally restricted to:
  - `POST /api/v1/edge/tap`
  - `GET /api/v1/health`

What was proven against that live deployment:

- browser-reachable HTTPS health returns `adapter=edge-projection`
- accepted `pass` taps update live occupancy
- repeated `in`, repeated `out`, `fail`, and stale rows stay deterministic
- `/metrics` reflects the same occupancy state
- identified publish still moves on NATS from the same accepted pass stream
- raw TouchNet replay can hit that same live `/api/v1/edge/tap` route

## Live Workstation Proof

The first bounded multi-workstation proof now includes:

- truthful facility and zone routing for Morningside:
  - `facility_id=morningside`
  - `zone_id=weight-room`
- truthful facility and zone routing for Ashtonbee:
  - `facility_id=ashtonbee`
  - `zone_id=gym-floor`
- workstation-neutral node IDs:
  - `ash-gym-01`
  - `ash-gym-02`
  - `ms-gym-01`
  - `ms-gym-02`
- direction inferred from the TouchNet row itself, not from the workstation
  name

Observed live behaviors from workstation testing:

- `ms-gym-01` and `ms-gym-02` can both emit real accepted and denied attempts
- accepted `in` and `out` rows publish the corresponding identified lifecycle
  subjects
- malformed accounts, wrong account-type attempts, and denied accounts stay
  observation-only
- repeated `out` rows produce `already_absent`
- repeated `in` rows produce `already_present`

Operational note:

- if `ATHENA_EDGE_TOKENS` changes through the live secret, restart the ATHENA
  deployment so the pod reloads the new node map
- if `ATHENA_EDGE_OBSERVATION_HISTORY_PATH` is enabled, a restart also replays
  committed `pass` observations before ATHENA serves traffic again

Restart / reload note:

- if the durable history file is unreadable, `athena serve` exits instead of
  claiming a rebuilt edge projection from a cold state
- durable-write failure during live ingress does not change the existing
  accepted / observed tap response or the publish / projection outcome

Compatibility note:

- the Chromebook workstation required a legacy-safe userscript variant because
  older Tampermonkey/runtime paths are more fragile than current Chrome on
  Windows
- keep workstation-specific live scripts local-only when they contain active
  node tokens; do not commit those token-bearing variants into the public repo
- if a node token changes in the live secret, update the local ignored live
  script for that workstation and restart the ATHENA deployment so the pod
  reloads the new node map

## Observed Fields

The edge bridge now forwards the full TouchNet row context into ATHENA for
operator diagnostics:

- `account_raw`
- `account_type`
- `name`
- `status_message`
- `result` as `pass` or `fail`
- inferred `direction` as `in` or `out`

Current behavior:

- `pass` rows are published as identified arrival or departure events
- `pass` rows also update the in-memory live occupancy projection when
  `ATHENA_EDGE_OCCUPANCY_PROJECTION=true`
- `fail` rows are accepted as observations and logged, but are not published to
  the current NATS visit-lifecycle subjects
- authorized rows can also be written into the Postgres durable observation
  store when `ATHENA_EDGE_POSTGRES_DSN` is set, with the older
  `ATHENA_EDGE_OBSERVATION_HISTORY_PATH` retained only as a local fallback
- `athena edge ingress-bridge` can classify existing Postgres observation,
  accepted-presence, projection, and source-pass session facts for future
  APOLLO trust gates without mutating APOLLO truth
- the durable record keeps `external_identity_hash`, not the raw account
  number, and stores only `name_present`, not the resolved name text or
  free-text `status_message`
- published visit events still use the hashed account as the canonical identity
  value, not the raw account number

Admin-facing note:

- ATHENA now has enough TouchNet context to support later operator or admin
  tooling for reconciliation between student number, RFID card number, name,
  and failure reasons
- ATHENA now exposes one bounded internal read API for privacy-safe facility
  history, but it does not expose a public or identity-level query API for
  that observed edge history
- if `Hermes` is the intended admin-facing surface, it is a reasonable place to
  add those operator endpoints later while ATHENA remains the ingestion and
  normalization boundary
- browser/userscript and replay event-id derivation may still drift today; the
  durable history path preserves the supplied `event_id` but does not claim to
  reconcile those variants yet

## Time Interpretation

- canonical event timestamps stay normalized in UTC
- runtime pod logs may be rendered in local Toronto time for operator clarity
- do not treat a local log timezone change as a change to stored or published
  event time semantics

## Required Checks

- `go test ./...`
- `go test -count=1 ./internal/edgehistory ./cmd/athena`
- `go test -count=1 ./internal/edge ./internal/edgehistory ./cmd/athena`
- `go test -count=5 ./internal/config ./internal/edge ./internal/touchnet`
- `go test -count=5 ./internal/presence ./internal/publish ./internal/server ./cmd/athena`

## Hardening-Proven Behaviors

The destructive hardening pass for this slice locally verified:

- first `in` updates projection and publishes `athena.identified_presence.arrived`
- repeated `in` stays observation-only and does not inflate occupancy
- `out` after `in` updates projection and publishes
  `athena.identified_presence.departed`
- repeated `out` stays observation-only and does not push occupancy below zero
- stale pass events stay observation-only
- `fail` taps stay observation-only and do not mutate occupancy
- facility and zone aggregates remain isolated
- replay through `athena edge replay-touchnet` drives the same `/api/v1/edge/tap`
  projection path
- emitted identified payloads contain the hashed identity only, not the raw
  account value
- durable history files contain the hashed identity only, not raw account
  values, resolved names, or free-text `status_message`
- durable-write failure is fail-open and does not change the existing tap
  outcome
- restart rebuild can restore the in-memory projection from committed `pass`
  observations before serving
- publish failure returns `503` and does not commit the in-memory projection

Use the hardening smoke sequence below when you need a closure-level recheck:

1. Start a local NATS server.
2. Start `athena serve` with `ATHENA_EDGE_OCCUPANCY_PROJECTION=true`.
3. Post one accepted `in` tap to `/api/v1/edge/tap`.
4. Verify `/api/v1/presence/count` and `/metrics`.
5. Post repeated `in`, accepted `out`, repeated `out`, `fail`, and stale taps.
6. Verify counts stay deterministic.
7. Replay a one-row TouchNet CSV through `athena edge replay-touchnet`.
8. Verify the replayed zone count and emitted subject.
9. Stop NATS and post one accepted `pass` tap.
10. Verify the HTTP response is `503` and the target zone count remains
    unchanged.

## Boundaries

- `/api/v1/presence/count` reads from the in-memory edge projection only when
  `ATHENA_EDGE_OCCUPANCY_PROJECTION=true`; adapter-backed read paths remain real
  outside that mode
- append-only durable history is activated only when
  `ATHENA_EDGE_OBSERVATION_HISTORY_PATH` is set
- APOLLO consumers remain unchanged
- `athena edge ingress-bridge` is CLI/internal-only; it does not create a
  public API, frontend/operator UI, schema/proto change, XP ledger, team
  behavior, or reliability scoring
- bounded live deployment truth is now proven for one facility and one node
  token, but the durable history branch is still local/runtime proof rather than
  bounded live deployment proof
- the userscript is intentionally DOM-based and does not capture raw keystrokes
