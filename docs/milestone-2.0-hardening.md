# Milestone 2.0 Hardening

Milestone 2.0 does not widen ATHENA into new product capability. It hardens the
existing runtime and keeps deploy truth explicit.

## Scope

- graceful shutdown for `athena serve`
- bounded HTTP server timeouts
- bounded publish retry/backoff
- bounded process-local publish dedupe memory
- no change to the current live edge-ingress contract

## Proof

Run from `/Users/zizo/Personal-Projects/ASHTON/athena`:

```sh
go test ./...
go test -count=5 ./internal/...
go test -race ./internal/...
go vet ./...
go build ./cmd/athena
git diff --check
```

Focused destructive coverage now includes:

- serve-command shutdown exits cleanly when the command context is canceled
- publish retries republish only the remaining messages after transient failure
- publish dedupe memory stays bounded instead of growing without limit
- bad node tokens, malformed tap payloads, repeated `in`, repeated `out`, and
  `fail` observations stay on the existing deterministic edge path
- history replay from committed `pass` observations stays deterministic when a
  history path is configured

## Negative Proof

Milestone 2.0 does not claim:

- a durable-history deploy rollout
- `ATHENA_EDGE_OBSERVATION_HISTORY_PATH` live in the current cluster
- Postgres-backed ingress storage
- prediction, analytics product surfaces, or broader operator UI

## Truth Split

- local/runtime truth: ATHENA is harder to stop safely and harder to wedge on
  transient publish failure on the `v0.6.1` patch line
- deployed truth: unchanged at the bounded `v0.4.1` live edge path
- deferred truth: durable-history deployment, Postgres ingress storage,
  prediction, and broader diagnostics
