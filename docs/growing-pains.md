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

## 2026-04-03

- Symptom: the first source-backed adapter draft would have made runtime results
  depend on raw CSV row order.
  Cause: file exports are a source detail, not a stable runtime ordering
  contract, but the first parser shape still preserved the incoming row order.
  Fix: sort parsed events by `recorded_at` and `event_id`, and reject duplicate
  `event_id` values so repeated loads stay deterministic.
  Rule: source-backed adapters must normalize ordering explicitly before the
  runtime depends on exported file shape.

- Symptom: the first CSV adapter draft almost reused mock-specific default
  facility config for the public read path.
  Cause: the original default filter lived in `ATHENA_MOCK_*` settings because
  mock was the only real adapter when the read path was first built.
  Fix: add adapter-agnostic `ATHENA_DEFAULT_FACILITY_ID` and
  `ATHENA_DEFAULT_ZONE_ID` while keeping the mock-specific settings for fixture
  generation.
  Rule: once more than one adapter is real, shared read-path defaults must stop
  depending on mock-only config names.

## 2026-04-04

- Symptom: the first TouchNet browser bridge draft only handled successful rows,
  which made it impossible to audit denied taps or reconcile operator actions
  later.
  Cause: the first spike treated the bridge as publish-only instead of
  observation-first ingress.
  Fix: forward both `pass` and `fail` rows into ATHENA, keep publishing limited
  to `pass`, and log the richer row context including `account_raw`,
  `account_type`, `name`, and `status_message`.
  Rule: for real facility ingress, ATHENA should preserve operator-observable
  truth even when downstream lifecycle publication stays intentionally narrow.

- Symptom: the first userscript draft assumed a generic table shape instead of
  the actual TouchNet Verify Account Entry DOM.
  Cause: the fixture was authored before enough real HTML had been captured from
  the page.
  Fix: retarget the script to the real selectors and cell positions, then keep
  a local fixture aligned with that actual page shape.
  Rule: browser-bridge automation should be anchored to saved real DOM evidence,
  not placeholder selectors.

## 2026-04-05

- Symptom: edge ingress and occupancy still behaved like two separate truths,
  which meant live taps could publish safely but `/api/v1/presence/count` kept
  reading from adapters only.
  Cause: the first edge slice stopped at publication and never fed a canonical
  normalized event into a live projection.
  Fix: add an explicit in-memory occupancy projector, normalize each `pass`
  edge tap once, and feed that same normalized event into both the projection
  updater and the identified publish builder.
  Rule: when ATHENA grows a new ingress truth, occupancy and publication should
  share one normalized event path instead of duplicating logic downstream.

- Symptom: the first attempt to make edge-driven occupancy real would have
  changed the read source implicitly for every `serve` process.
  Cause: the existing runtime already had real mock/csv adapter paths, so
  silently swapping them out would have blurred what was actually deployed and
  what was only proven locally.
  Fix: gate edge-driven occupancy behind explicit
  `ATHENA_EDGE_OCCUPANCY_PROJECTION=true` serve config and keep adapter-backed
  CLI/read paths intact outside that mode.
  Rule: ATHENA source-of-truth changes must be explicit runtime choices, not
  accidental side effects of enabling ingress.

- Symptom: repeated `in` rows could have inflated occupancy and double-published
  identified arrivals if the live projection and publish path stayed
  independent.
  Cause: publish eligibility was based only on row validity, not on whether the
  projected presence state actually changed.
  Fix: make the projector reject `already_present`, `already_absent`, and stale
  events deterministically, and only publish after the projector accepts the
  transition.
  Rule: the live edge path must be deterministic under duplicate, conflicting,
  and out-of-order taps before it can be treated as occupancy truth.

- Symptom: a broker failure during an accepted `pass` tap could have committed
  projection state before the identified publish actually succeeded.
  Cause: the first projection draft treated publish and projection as separate
  side effects, which would have made retry behavior incorrect under a NATS
  outage.
  Fix: move the publish side effect into the projector commit path so
  projection only commits after publish succeeds, and verify the broker-down
  path returns `503` without mutating occupancy.
  Rule: when one normalized event drives both live occupancy and downstream
  publish, the commit point must preserve retry safety under partial failure.

## 2026-04-06

- Symptom: the first deployment follow-through stalled even though
  `athena v0.4.1` was already built and tagged.
  Cause: repo/runtime truth and deployable artifact truth were separate; the
  cluster was still pinned to `0.4.0`, the local GHCR credential had lost
  package scopes, and Kubernetes had no image pull credential for private
  GHCR access.
  Fix: restore package-capable local publish auth, publish the exact `v0.4.1`
  image digest, and wire a dedicated cluster-only GHCR pull secret through a
  ServiceAccount `imagePullSecrets` path in GitOps.
  Rule: a release tag is not deployment truth until the artifact is actually
  pullable by the cluster runtime that will use it.

- Symptom: the first browser-reachable ATHENA ingress idea would have widened
  the public surface too far by exposing the whole service externally.
  Cause: the raw `athena` service already serves health, occupancy, metrics, and
  ingress, but the browser/Tampermonkey requirement only needed the tap path.
  Fix: place a narrow proxy in front of ATHENA and expose only
  `/api/v1/edge/tap` and `/api/v1/health` through the quick tunnel.
  Rule: when proving browser reachability for an ingress boundary, expose the
  minimum public surface that proves the slice and nothing broader.
