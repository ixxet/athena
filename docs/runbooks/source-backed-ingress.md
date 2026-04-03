# ATHENA Source-Backed Ingress Runbook

## Purpose

Use this runbook to prove the first source-backed ATHENA ingress slice:
`csv export -> adapter -> canonical occupancy read path`.

## Source Shape

The first real adapter consumes a bounded CSV presence-event export with these
columns:

- `event_id`
- `facility_id`
- `zone_id` optional
- `external_identity_hash` optional
- `direction`
- `recorded_at`

`direction` must be `in` or `out`. `recorded_at` must be RFC3339 or
RFC3339Nano.

## Fixture

Start from the tracked sample file:

`docs/runbooks/fixtures/source-backed-presence.csv`

Copy it to a temporary location before destructive smoke so the bad-source check
does not dirty the repo.

## Happy Path

```bash
cd /Users/zizo/Personal-Projects/ASHTON/athena
tmpdir=$(mktemp -d)
cp docs/runbooks/fixtures/source-backed-presence.csv "$tmpdir/presence.csv"

ATHENA_ADAPTER=csv \
ATHENA_CSV_PATH="$tmpdir/presence.csv" \
ATHENA_DEFAULT_FACILITY_ID=ashtonbee \
ATHENA_HTTP_ADDR=127.0.0.1:18090 \
go run ./cmd/athena serve
```

In another shell:

```bash
curl -sS -i http://127.0.0.1:18090/api/v1/health
curl -sS -i http://127.0.0.1:18090/api/v1/presence/count
curl -sS -i 'http://127.0.0.1:18090/api/v1/presence/count?facility=other'
```

Expected results:

- health returns `200` with `"adapter":"csv"`
- default occupancy returns `facility_id=ashtonbee` and `current_count=1`
- explicit `facility=other` returns `current_count=1`
- logs show:
  - `csv adapter initialized`
  - `csv adapter refreshed`
  - `starting ATHENA server`

## Failure Path

Corrupt the live source file:

```bash
cat > "$tmpdir/presence.csv" <<'EOF'
event_id,facility_id,direction,recorded_at
csv-in-001,ashtonbee,sideways,2026-04-01T08:00:00Z
EOF
```

Then rerun the same read:

```bash
curl -sS -i http://127.0.0.1:18090/api/v1/presence/count
```

Expected result:

- HTTP `500`
- body includes `direction "sideways" must be one of in,out`
- logs include `csv adapter refresh failed`

Startup should also fail clearly when the configured source file is missing:

```bash
ATHENA_ADAPTER=csv \
ATHENA_CSV_PATH=/tmp/tracer10-missing.csv \
ATHENA_DEFAULT_FACILITY_ID=ashtonbee \
ATHENA_HTTP_ADDR=127.0.0.1:18090 \
go run ./cmd/athena serve
```

Expected result:

- process exits non-zero
- stderr includes `read csv source`

## Required Checks

- `go test ./...`
- `go test -count=5 ./internal/adapter ./internal/config ./cmd/athena`
- `go test -count=5 ./internal/server ./internal/presence`
- `go test -count=5 ./internal/publish`
- `go build ./cmd/athena`

## Boundaries

- the CSV adapter is a local runtime proof only
- deployed truth remains unchanged until a later bounded deployment workstream
- the adapter remains physical-truth only and does not infer lobby, workout, or
  recommendation state
