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
- bounded live in-cluster arrival proof is real through Milestone 1.5
- deployed truth is unchanged; the live read path still does not claim
  source-backed ingress

## Planned Release Lines

| Planned tag | Intended purpose | Restrictions | What it should not do yet |
| --- | --- | --- | --- |
| `v0.4.1` | source-backed deployment and live departure-close support | only widen deployed truth as far as the bounded workstream proves | do not imply broad ATHENA ingress rollout or broader APOLLO product deployment |
| `v0.5.0` | persistence and broader diagnostics | activate Postgres-backed state only when a tracer needs it | do not mix storage activation with prediction rollout |
| `v0.6.0` | capacity prediction runtime | build on stable ingress and event history first | do not ship dashboards or predictive UX before prediction itself is real |

## Boundaries

- Tracer 9 does not require ATHENA widening by default
- Tracer 10 keeps ingress physical-truth only and does not widen into
  deployment, prediction, or social logic
- keep physical truth separate from member intent and product logic
- do not activate Redis-backed hot counters before the basic read path needs them
- do not widen into predictive dashboards before prediction is real

## Tracer / Workstream Ownership

- `Tracer 1`: first mock-backed read line
- `Tracer 2`: identified arrival publication
- `Tracer 5`: identified departure publication
- `Tracer 10`: first real ingress adapter
- `Milestone 1.6`: live departure-close proof in-cluster
- later line: persistence and prediction after source-backed ingress is stable
