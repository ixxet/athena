# ATHENA Adapter Drift Handoff Pack

Use this pack when a chat started as TouchNet adapter or workstation work and
ended up proving broader ATHENA deployment truth. The goal is to feed that work
back into the organizer chat without corrupting the tracer ladder.

This pack is intentionally hybrid:

- workstream-first for the actual closeout
- tracer-aware so the organizer can keep the ladder honest

## Quick Truth Ledger

- `athena v0.4.1` is already a shipped runtime and bounded deployment truth for
  the live edge-ingress slice.
- This chat extended that line operationally with real workstation/browser usage
  and live deployment proof.
- This chat did **not** create a new ATHENA runtime capability line.
- This chat did **not** consume `Tracer 15`.
- `Tracer 15` is still the gateway caller-identity and persisted-audit line in
  `ashton-platform`.
- `Tracer 16` remains the next ATHENA capability line and is currently about
  durable edge-observation groundwork, not product-social features.
- Durable-history and social-signal discussion from this chat is planning
  context only, not implemented truth.

## Pack Map

| Pack | Best use | Main scope | Must read first | Must avoid |
| --- | --- | --- | --- | --- |
| Organizer ingest pack | absorb this chat back into the head-organizer chat | workstream closeout truth, tracer-status note, next-line integrity | `IMPLEMENTATION-BOARD.md`, `ashton-platform/README.md`, `athena/README.md`, `docs/roadmap.md`, `docs/runbooks/edge-ingress.md` | accidentally marking this chat as `Tracer 15` or `Tracer 16` execution |
| Worker bootstrap pack | start the next ATHENA durability worker chat | `Tracer 16` scope, hard stop, rollout assumptions | `athena/README.md`, `docs/roadmap.md`, `docs/edge-observation-history-plan.md`, `ashton-platform` control-plane lines | product widening, reporting-first assumptions, breaking live ingress |
| Handoff contract | glue between worker and organizer chats | exact return shape so the ladder stays accurate | both packs | fuzzy closure claims and implied release movement |

## Organizer Ingest Pack

```text
ATHENA ADAPTER + LIVE EDGE DEPLOYMENT WORKSTREAM CLOSEOUT

Role:
You are the organizer chat absorbing one ATHENA workstream that drifted outside
the main tracer sequence.

Do not let this workstream corrupt the tracer ladder.
Do not silently reclassify it as `Tracer 15` or `Tracer 16`.

Read first:
1. /Users/zizo/Personal-Projects/ASHTON/ashton-platform/planning/IMPLEMENTATION-BOARD.md
2. /Users/zizo/Personal-Projects/ASHTON/ashton-platform/README.md
3. /Users/zizo/Personal-Projects/ASHTON/athena/README.md
4. /Users/zizo/Personal-Projects/ASHTON/athena/docs/roadmap.md
5. /Users/zizo/Personal-Projects/ASHTON/athena/docs/runbooks/edge-ingress.md
6. /Users/zizo/Personal-Projects/ASHTON/athena/docs/touchnet-edge-handoff-prompt.md

Primary label:
- ATHENA adapter + live edge deployment workstream closeout

Tracer-status note:
- no `Tracer 15` scope was consumed
- `Tracer 16` planning context was created but not implemented

Exact truth to preserve:
- the live TouchNet browser bridge is real
- bounded live ATHENA deployment proof is real
- live taps are flowing from Morningside workstations now
- durable edge history is still not implemented

What this chat actually accomplished:
- per-node browser bridge userscripts were proven
- Windows and Chromebook legacy script variants were both proven
- node-level posting now works for:
  - `ms-gym-01`
  - `ms-gym-02`
- direction inference from the TouchNet row text is real
- live taps from Morningside are flowing now
- current retained history is still weak because taps live in logs/in-memory,
  not durable storage

Formal tracer ruling:
- this chat did not accidentally become `Tracer 15`
- `Tracer 15` remains the gateway caller-identity and persisted-audit line per:
  - /Users/zizo/Personal-Projects/ASHTON/ashton-platform/planning/IMPLEMENTATION-BOARD.md
  - /Users/zizo/Personal-Projects/ASHTON/ashton-platform/README.md
- `Tracer 16` remains the next ATHENA line

Release-line truth:
- `athena v0.4.1` is already the shipped runtime/deployment line for the live
  edge-ingress slice
- this workstream extended that line operationally, but did not create a new
  formal ATHENA runtime capability line

Deployment note:
- Prometheus deployment truth changed materially and should be remembered as
  part of this ATHENA workstream outcome
- deployment hygiene and future secret/token cleanup remain deferred notes, not
  closure blockers

Organizer job after ingest:
1. record this as a bounded ATHENA workstream layered on top of shipped
   `athena v0.4.1`
2. keep `Tracer 15` untouched unless actual gateway work later lands
3. treat `Tracer 16` as the next formal ATHENA line
4. carry forward the live ingress/deployment truth so the next ATHENA worker
   does not rediscover it
```

## Worker Bootstrap Pack

```text
ATHENA WORKER BOOTSTRAP: TRACER 16 DURABLE EDGE HISTORY

Role:
You are an ATHENA worker chat for the next bounded ATHENA line.
You are not closing the earlier adapter/deployment workstream.
You are starting from the truth it already established.

Owned line:
- `Tracer 16`
- target repo: `/Users/zizo/Personal-Projects/ASHTON/athena`
- supporting control-plane references:
  - `/Users/zizo/Personal-Projects/ASHTON/ashton-platform/planning/IMPLEMENTATION-BOARD.md`
  - `/Users/zizo/Personal-Projects/ASHTON/ashton-platform/README.md`
- deployment repo mention only for carry-forward context:
  - `/Users/zizo/Personal-Projects/Computers/Prometheus`

Read first:
1. `/Users/zizo/Personal-Projects/ASHTON/athena/README.md`
2. `/Users/zizo/Personal-Projects/ASHTON/athena/docs/roadmap.md`
3. `/Users/zizo/Personal-Projects/ASHTON/athena/docs/edge-observation-history-plan.md`
4. `/Users/zizo/Personal-Projects/ASHTON/athena/docs/runbooks/edge-ingress.md`
5. `/Users/zizo/Personal-Projects/ASHTON/ashton-platform/planning/IMPLEMENTATION-BOARD.md`
6. `/Users/zizo/Personal-Projects/ASHTON/ashton-platform/README.md`

Starting truth:
- `athena v0.4.1` live edge ingress is already real
- browser/tunnel/token/workstation proof already exists
- live taps are flowing now
- durable history is still missing

Hard stop:
- durable edge observation history
- session inference groundwork
- privacy-safe history proof

Not in scope:
- prediction
- override tooling
- identity reconciliation UI
- product/social widening
- report-first or public-query-first surfaces
- any claim that social signals are already runtime truth

Critical rollout assumption:
- keep the existing live browser/tunnel/token path stable
- persistence must not casually break the current live tap pipeline
- the first durability rollout needs an explicit failure posture decision
  before implementation

Worker quality bar:
- separate local truth, deployed truth, and deferred truth
- do not overclaim durable history if the runtime is still log-only
- do not claim public report or export surfaces unless they are truly built
- do not let `Tracer 16` drift into APOLLO-style social features
```

## Handoff Contract

```text
HANDOFF CONTRACT: WORKER -> ORGANIZER

Worker must return:
1. exact line executed
   - `Tracer 16` or a narrower named workstream if tracer execution was
     deliberately deferred
2. repo truth
   - clean or not
   - on `main` or not
   - pushed commit(s)
3. release impact
   - whether ATHENA actually moved to `v0.5.0`
4. truth split
   - local truth
   - deployed truth
   - deferred truth
5. docs touched
   - ATHENA repo docs
   - platform docs only if shared truth changed
6. hardening evidence
   - deterministic persistence behavior
   - restart/reload story
   - no raw-ID leakage regression
7. carry-forward gaps
   - explicit list only

Organizer must do with it:
1. keep `Tracer 15` untouched unless gateway work really happened
2. absorb the ATHENA workstream into the ladder without renumbering prior lines
3. update platform truth only if the worker actually changes the formal
   next-line state
4. do not let planning drift be rewritten as runtime truth

Acceptance test for this contract:
- an organizer chat can answer immediately whether `Tracer 15` moved
- a worker chat can start `Tracer 16` without rediscovering live ATHENA ingress
  and deployment truth
- completed adapter/deployment work, formal tracer state, and deferred durable
  history work remain clearly separated
```

## Notes

- Mention Prometheus only as supporting deployment closure context, not as the
  main next scope.
- Treat `docs/touchnet-edge-handoff-prompt.md` as the older narrower spike
  prompt, not the final closeout artifact for the broader live workstream.
- Use this pack when you need to preserve the truth of this chat without
  pretending a gateway tracer or durable-history tracer was already executed.
