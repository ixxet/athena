# athena Roadmap

## Objective

Keep ATHENA moving through narrow physical-truth slices: read path first, then
publication, then real ingress, then persistence and prediction only when they
are earned.

## Current Line

Current active line: `v0.5.1` shipped.

`v0.5.0` remains the Tracer 16 durable-history release line. `v0.5.1` is the
bounded Tracer 17 support follow-up on that same durable-history branch.

- mock-backed occupancy read path is still real
- one source-backed CSV ingress adapter is now real locally
- identified arrival and departure publication is real
- explicit in-memory edge-driven occupancy projection is now real for
  `athena serve`
- one bounded internal HTTP facility-history read is now real on top of the
  existing durable-history groundwork to support HERMES reconciliation without
  widening into a public operator surface
- bounded live in-cluster arrival proof is real through Milestone 1.5
- bounded live browser-reachable ATHENA edge ingress and occupancy proof is now
  real for `v0.4.1`
- optional local file-backed durable history and restart replay groundwork now
  exist behind explicit config
- bounded live proof still does not include the durable branch, and broad
  ingress rollout is still unproven

## Current And Planned Release Lines

| Release line | Intended purpose | Restrictions | What it should not do yet |
| --- | --- | --- | --- |
| `v0.5.1` | bounded privacy-safe facility-history support follow-up on the existing durable-history line | keep the new route facility-filtered, read-only, and subordinate to durable-history truth | do not imply a public operator surface, identity-level reconciliation, or durable-branch deployment |
| `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads | build on stable ingress and trusted durable history first | do not widen into social logic or broad product UX |
| later than `v0.6.0` | broader diagnostics and capacity prediction runtime | build on stable ingress, trusted durable history, and clean facility truth first | do not ship dashboards or predictive UX before prediction itself is real |

## Next Ladder Role

| Line | Role | Why it matters |
| --- | --- | --- |
| Tracer 17 support follow-up / `v0.5.1` | bounded privacy-safe facility-history support for HERMES reconciliation | lets HERMES consume durable history without private file access or broader ATHENA widening |
| `Tracer 18` / `v0.6.0` | facility catalog, hours, zones, closure windows, and per-facility metadata reads | gives later sports, scheduling, and reporting logic trustworthy facility truth |
| later than `v0.6.0` | broader diagnostics and capacity prediction runtime | keeps prediction later than trusted ingress, durable history, and facility truth |

## Boundaries

- Tracer 9 does not require ATHENA widening by default
- Tracer 10 keeps ingress physical-truth only and does not widen into
  deployment, prediction, or social logic
- the edge-driven occupancy slice keeps projection in memory by default; the
  new append-only file-backed history path is explicit and optional
- the bounded live deployment uses one facility, one node token, a private GHCR
  pull secret, and a narrow HTTPS proxy path; it is not yet a broad ATHENA
  ingress rollout
- observed TouchNet pass/fail history is a plausible next ATHENA persistence
  slice, but operator override workflows should remain separate from the
  physical-truth ingest boundary
- the new bounded history read must stay privacy-safe, facility-filtered, and
  read-only; it exists to support HERMES rather than to turn ATHENA into a
  broad operator product surface
- duration-of-stay, tap frequency, and workstation-quality metrics should be
  derived from durable append-only observation history, not from ad hoc log
  scraping
- keep physical truth separate from member intent and product logic
- do not activate Redis-backed hot counters before the basic read path needs them
- do not widen into predictive dashboards before prediction is real

## Tracer / Workstream Ownership

- `Tracer 1`: first mock-backed read line
- `Tracer 2`: identified arrival publication
- `Tracer 5`: identified departure publication
- `Tracer 10`: first real ingress adapter
- separate ATHENA ingress slice: edge-driven occupancy projection from the same
  normalized event stream used for identified publish
- bounded deployment workstream: live browser-reachable HTTPS ingress for that
  same `v0.4.1` edge path
- that bounded deployment workstream is real deployment truth, but it did not
  consume a tracer number and should not be treated as partial `Tracer 16`
- `Milestone 1.6`: live departure-close proof in-cluster
- `Tracer 18`: facility metadata and hours after durable history is stable
- later line: broader diagnostics and prediction after source-backed ingress and facility truth are stable
