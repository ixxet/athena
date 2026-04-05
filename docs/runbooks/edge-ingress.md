# ATHENA Edge Ingress Runbook

## Purpose

Use this runbook to prove the new push-based ATHENA ingress slice:

`TouchNet-shaped source -> POST /api/v1/edge/tap -> NATS -> APOLLO`

The canonical occupancy read path remains unchanged in this slice. This runbook
is only about identified arrival and departure publication.

## Environment

Start ATHENA with NATS plus edge-ingress config:

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena

ATHENA_HTTP_ADDR=127.0.0.1:18090 \
ATHENA_ADAPTER=mock \
ATHENA_NATS_URL=nats://127.0.0.1:4222 \
ATHENA_EDGE_HASH_SALT=demo-salt \
ATHENA_EDGE_TOKENS='entry-node=entry-token,exit-node=exit-token' \
go run ./cmd/athena serve
```

If the edge config is valid, `/api/v1/edge/tap` is mounted automatically.

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
7. Confirm that only `Pass` rows are posted and repeated rerenders do not
   duplicate accepted events.

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
- `go test -count=5 ./internal/publish ./internal/server ./cmd/athena`

## Boundaries

- `/api/v1/presence/count` still reads from the configured adapter
- no ATHENA persistence is activated in this slice
- APOLLO consumers remain unchanged
- the userscript is intentionally DOM-based and does not capture raw keystrokes
