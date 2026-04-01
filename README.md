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

Bootstrap slice complete:

- `go.mod` exists
- mock presence adapter exists
- one CLI count command exists
- one HTTP read endpoint exists
- one Prometheus metric exists
- container build and GitHub Actions image publishing exist

See:

- `docs/roadmap.md`
- `docs/runbooks/mock-slice.md`
- `docs/growing-pains.md`
