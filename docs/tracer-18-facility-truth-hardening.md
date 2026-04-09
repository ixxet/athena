# Tracer 18 Facility Truth Hardening

## Scope

Tracer 18 adds bounded read-only facility truth to ATHENA:

- facility catalog
- facility hours
- facility zones
- facility closure windows
- per-facility metadata

It does not add sport logic, scheduling runtime, product semantics, writes, or
prediction.

## Implementation Shape

- source of truth: validated JSON catalog pointed to by `ATHENA_FACILITY_CATALOG_PATH`
- CLI: `athena facility list`, `athena facility show --facility <id>`
- internal HTTP: `GET /api/v1/facilities`, `GET /api/v1/facilities/{facility_id}`
- no `ashton-proto` widening
- no Postgres activation

## Proof Commands

```bash
go test ./...
go test -count=5 ./internal/...
go vet ./...
go build ./cmd/athena
git diff --check
```

## Destructive Checks

- invalid facility IDs reject cleanly
- malformed or overlapping hours reject cleanly
- missing zones reject cleanly
- closure windows with bad zone references reject cleanly
- conflicting closure windows reject cleanly
- missing metadata rejects cleanly
- existing occupancy and edge-history truth remain unchanged

## Truth Split

- local/runtime truth: facility catalog reads are real when the catalog path is configured
- deployed truth: unchanged from the earlier bounded edge-ingress line
- deferred truth: public/demo surfaces, scheduling answers, sport capability, writes, overrides, prediction, and shared contract growth
