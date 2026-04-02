# Growing Pains

Use this document to record real incidents, broken assumptions, adapter failures,
prediction mistakes, and the fixes that made `athena` more operationally solid.

## 2026-04-01

- The first image build failed because `go.mod` required Go `>= 1.23` while the
  Dockerfile still used `golang:1.22`. The fix was to align the builder image
  with the actual module toolchain requirement before retrying the build.

- The first safe GitOps activation started at `replicas: 0` until a real image,
  smoke test, and digest pin existed. This prevented turning an unverified
  placeholder into a broken live deployment.

- The first GHCR-published image was single-platform only. It deployed cleanly
  for the cluster path, but local smoke on an arm64 Mac failed with `no matching
  manifest for linux/arm64/v8`. The fix is to either publish a multi-arch image
  or explicitly test the amd64 image through emulation when local validation is
  required.

- The first occupancy gauge update path lived inside the HTTP handler, which
  meant a filtered API read could change the value Prometheus scraped later. The
  fix was to make the metric read from the same canonical default occupancy path
  that CLI and HTTP use, instead of mutating shared gauge state from requests.

- The first mock adapter seeded timestamps directly from `time.Now()`, which
  made tests and read outputs less stable than they needed to be. The fix was to
  allow fixed base times and explicit event fixtures so the narrow slice is
  deterministic under test.

- The first Tracer 2 publisher draft would have republished the same static mock
  arrivals on every `serve` poll. The fix was to keep a process-local published
  id cache in the worker and leave cross-restart replay handling to APOLLO
  idempotency.

## 2026-04-02

- Symptom: the identified-arrival publisher still owned a private JSON contract
  even after `ashton-proto` defined the shared event.
  Cause: the first Tracer 2 pass stopped at a working wire shape instead of
  finishing the runtime contract-sharing loop.
  Fix: switch the publisher onto the shared `ashton-proto/events` helper and
  map ATHENA source values explicitly into the shared enum.
  Rule: ATHENA owns physical truth, not a second copy of the shared event wire
  contract.

- Symptom: `athena presence publish-identified` returned an error against real
  NATS even though APOLLO still recorded the visit.
  Cause: `FlushWithContext` was called with Cobra's root context, which had no
  deadline, so publish reporting failed after the message had already been sent.
  Fix: add a bounded flush timeout when the caller context has no deadline and
  cover that branch with a regression test.
  Rule: every broker flush path needs an explicit deadline, and one-shot publish
  commands should be smoke-tested against a real broker before a tracer closes.

- Symptom: the Tracer 5 departure publisher code compiled in `ashton-proto`,
  but ATHENA still failed to build against the new shared contract symbols.
  Cause: this repo intentionally pins `ashton-proto` as a module dependency
  instead of assuming a local workspace replace, so the new departure helper and
  proto types were not available until the dependency moved forward too.
  Fix: bump the `ashton-proto` module in the same tracer that starts using new
  shared contract symbols, then rerun the publish and CLI suites.
  Rule: when ATHENA adopts a new shared contract surface, the producer code and
  the module pin must move together in one verified change.
