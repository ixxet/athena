# athena Roadmap

## Objective

Keep ATHENA moving through narrow physical-truth slices: read path first, then
publication, then real ingress, then persistence and prediction only when they
are earned.

## Current Line

Current active line: `v0.4.x`

- mock-backed occupancy read path is still real
- one source-backed CSV ingress adapter is now real locally
- identified arrival and departure publication is real
- explicit in-memory edge-driven occupancy projection is now real for
  `athena serve`
- bounded live in-cluster arrival proof is real through Milestone 1.5
- bounded live browser-reachable ATHENA edge ingress and occupancy proof is now
  real for `v0.4.1`
- persistence is still deferred and broad ingress rollout is still unproven

## Planned Release Lines

| Planned tag | Intended purpose | Restrictions | What it should not do yet |
| --- | --- | --- | --- |
| `v0.4.2` | durable edge-observation groundwork, session analytics planning, and ingress hardening | only widen deployed truth as far as the bounded workstream proves | do not imply broad ingress rollout, override workflows, or a finished persistence story before append-only history is actually active |
| `v0.5.0` | persistence and broader diagnostics | activate Postgres-backed state only when a tracer needs it | do not mix storage activation with prediction rollout |
| `v0.6.0` | capacity prediction runtime | build on stable ingress and event history first | do not ship dashboards or predictive UX before prediction itself is real |

## Next Ladder Role

| Line | Role | Why it matters |
| --- | --- | --- |
| `Tracer 16` | durable edge-observation groundwork, session inference, and ingress hardening | reduces all-memory dependence, turns tap history into queryable operational truth, and sets up dwell and flow analytics without widening product logic |
| `v0.5.0` | persistence and broader diagnostics | turns the bounded live edge path into durable operational history |
| `v0.6.0` | capacity prediction runtime | keeps prediction later than trusted ingress and durable history |

## Boundaries

- Tracer 9 does not require ATHENA widening by default
- Tracer 10 keeps ingress physical-truth only and does not widen into
  deployment, prediction, or social logic
- the edge-driven occupancy slice keeps projection in memory only; no append-only
  observation storage or occupancy snapshot persistence is active yet
- the bounded live deployment uses one facility, one node token, a private GHCR
  pull secret, and a narrow HTTPS proxy path; it is not yet a broad ATHENA
  ingress rollout
- observed TouchNet pass/fail history is a plausible next ATHENA persistence
  slice, but operator override workflows should remain separate from the
  physical-truth ingest boundary
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
- `Milestone 1.6`: live departure-close proof in-cluster
- later line: persistence and prediction after source-backed ingress is stable
