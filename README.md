# athena

ATHENA is the first executable service in ASHTON. It owns facility presence, ingress source handling, occupancy visibility, capacity prediction, and the first real operational data surface that the rest of the platform will depend on.

This repo is now past pure docs-first bootstrap. The detailed brief lives in [ashton-platform/planning/repo-briefs/athena.md](https://github.com/ixxet/ashton-platform/blob/main/planning/repo-briefs/athena.md).

## Role In The Platform

- first implementation repo
- depends on `ashton-proto`
- owns physical truth for presence and occupancy
- produces the presence and occupancy data that HERMES and APOLLO consume later

## First Execution Goal

Ship the mock physical-truth tracer bullet:

- mocked presence input
- presence updates separate from matchmaking intent
- current occupancy read path
- one CLI command
- one HTTP read endpoint
- one Prometheus metric

## Planned Redis Use

Redis is deferred from the first tracer bullet, but it remains a planned utility for fast occupancy counters and short-lived aggregate caching once ATHENA has a real event flow.

## Current State

Tracer 1 read-path hardening is in place:

- the mock adapter supports deterministic fixtures for tests
- one canonical occupancy read path is shared by CLI, HTTP, and Prometheus
- unknown facilities return a deterministic zero count instead of panicking or drifting negative
- invalid adapter config fails fast at startup
- container builds now follow the target architecture so local arm64 smoke and published multi-arch images can converge
- Tracer 2 adds one identified-arrival publish path reused by `athena presence publish-identified` and an optional `serve` worker without changing the occupancy read path
- Tracer 2 closure hardening moves the publish path onto the shared
  `ashton-proto` runtime contract and adds explicit publish and reject logging
- local manual smoke passed for both `athena presence publish-identified` and
  `athena serve` with the publisher worker against a live `apollo serve` + NATS
  + Postgres setup
- this repo is ready for a `v0.2.1` tracer-close tag

See:

- `docs/roadmap.md`
- `docs/runbooks/mock-slice.md`
- `docs/growing-pains.md`
