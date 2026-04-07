# ATHENA Edge Ingress Runbook

## Purpose

Use this runbook to prove the new push-based ATHENA ingress slice:

`TouchNet-shaped source -> POST /api/v1/edge/tap -> in-memory occupancy projection + NATS -> APOLLO`

In explicit edge-projection mode, the same normalized pass event now drives:

- live occupancy projection for `/api/v1/presence/count` and `/metrics`
- identified arrival or departure publication to NATS

Persistence is still deferred in this slice.

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
go run ./cmd/athena serve
```

If the edge config is valid, `/api/v1/edge/tap` is mounted automatically and
`/api/v1/presence/count` now reads from the in-memory edge projection.

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

## Browser Fixture

Use these files before the facility reopens:

- `docs/runbooks/fixtures/touchnet-edge.user.js`
- `docs/runbooks/fixtures/touchnet-edge-fixture.html`

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

## Chrome Quick Start

For the real TouchNet page:

1. Open the Verify Account Entry page in Chrome.
2. Open Tampermonkey, enable the `Ashton TouchNet Edge Bridge` script, and save it.
3. Set the script values:
   - `baseUrl` to your local ATHENA URL, usually `http://127.0.0.1:18090`
   - `nodeId` to the node configured in `ATHENA_EDGE_TOKENS`
   - `token` to the token paired with that node
4. Refresh the TouchNet page once after saving the script.
5. Keep cursor focus in the account field. The script shows a red banner if focus leaves `#verify_account_number`.
6. Scan a card or type a number and press Enter.
7. Watch ATHENA logs for `edge tap accepted`.

The userscript posts through Tampermonkey's cross-origin request API, so it is
better suited to a real browser test than plain in-page `fetch()`.

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
- published visit events still use the hashed account as the canonical identity
  value, not the raw account number

Admin-facing note:

- ATHENA now has enough TouchNet context to support later operator or admin
  tooling for reconciliation between student number, RFID card number, name,
  and failure reasons
- ATHENA does not yet expose a read API for that observed edge history
- if `Hermes` is the intended admin-facing surface, it is a reasonable place to
  add those operator endpoints later while ATHENA remains the ingestion and
  normalization boundary

## Required Checks

- `go test ./...`
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
- no ATHENA persistence is activated in this slice
- APOLLO consumers remain unchanged
- bounded live deployment truth is now proven for one facility and one node
  token, but broad ingress rollout and persistence are still deferred
- the userscript is intentionally DOM-based and does not capture raw keystrokes
