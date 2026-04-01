# ATHENA Mock Slice Runbook

## Purpose

Use this runbook for the first narrow ATHENA slice and future mock-backed read-path work.

## Minimum Flow

1. mock adapter emits presence events
2. service computes current occupancy
3. CLI exposes the same value
4. HTTP read endpoint exposes the same value
5. Prometheus metric reflects current occupancy

## Required Checks

- `go test ./...`
- container smoke test for `/api/v1/health`, `/api/v1/presence/count`, and `/metrics`
- manifest render via `kustomize build` or `kubectl kustomize`

## Do Not Widen Until

- real count logic is stable
- edge cases like negative occupancy are covered
- container build and image publish path are proven
