# TouchNet Edge Handoff Prompt

Use the prompt below to hand this blocker to another chat or engineer without
losing the actual implementation boundary.

## Ready-To-Paste Prompt

```text
I need you to continue a TouchNet-to-ATHENA edge-ingress spike that is already implemented in the ATHENA repo.

What is already done:

- A Tampermonkey browser bridge watches the real TouchNet "Verify Account Entry" page.
- It reads each new TouchNet transaction row and POSTs it to ATHENA at `/api/v1/edge/tap`.
- ATHENA authenticates the browser client with a per-node token.
- ATHENA immediately hashes the raw account value and uses that hash as the canonical downstream identity.
- ATHENA publishes only `pass` rows as identified arrival/departure events to NATS.
- ATHENA also accepts `fail` rows as observations so operator-visible failures are not lost.

What the browser bridge captures from TouchNet:

- `account_raw`
- `account_type`
- `name`
- `status_message`
- `result` as `pass` or `fail`
- inferred `direction` as `in` or `out`
- `observed_at`
- `node_id`

What ATHENA does with those fields today:

- logs a redacted local observation with hashed identity plus bounded operator
  context
- hashes `account_raw` immediately into `external_identity_hash`
- publishes only the hashed identity plus facility/zone/source/time downstream
- does not currently expose a query API for observed edge history
- does not currently persist those observations in a database; the local proof stores them in logs only

Important current design boundary:

- ATHENA is the ingestion and normalization boundary
- ATHENA should preserve original TouchNet truth
- ATHENA should not silently convert a `fail` into a `pass`
- if staff let someone in despite a TouchNet deny, that should be a separate override record, not a mutation of the original observed row

What needs to be reasoned about next:

1. Should ATHENA persist immutable edge observations in Postgres?
2. If yes, design an append-only observation table that stores the raw TouchNet context losslessly.
3. Design a separate override / manual-admit model instead of rewriting original TouchNet outcomes.
4. Decide whether HERMES should own the admin-facing search / audit / override endpoints.
5. Decide whether `ashton-proto` should stay hash-only downstream or whether any richer contract is justified.

Suggested storage model:

- `edge_observations`
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

- `edge_overrides`
  - `override_id`
  - `edge_event_id`
  - `action`
  - `operator_id`
  - `reason`
  - `notes`
  - `created_at`

Repo ownership guidance:

- `athena`: ingest, hashing, immutable observation history, canonical visit-lifecycle publication
- `hermes`: admin/operator search, review, reconciliation, manual admit / exception workflows
- `ashton-proto`: only if downstream contract expansion is truly needed

Relevant ATHENA docs to read first:

- `docs/touchnet-edge-spike.md`
- `docs/runbooks/edge-ingress.md`
- `docs/touchnet-edge-handoff-prompt.md`
- `README.md`

Please continue from the existing ATHENA implementation instead of redesigning the spike from scratch. Be explicit about what is already real, what is still log-only, and what should become the next narrow persistent slice.
```

## Notes

This handoff prompt is intentionally precise about current behavior:

- capture and ingest are real
- lossless persistent storage is **not** real yet
- raw account values are no longer emitted in routine edge logs today
- downstream publish still uses the hashed identity only
- manager or staff overrides belong in a separate operational workflow, not as a rewrite of the original TouchNet outcome
