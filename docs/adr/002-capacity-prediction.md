# ADR 002: Capacity Prediction Strategy

## Status

Accepted as design, deferred in implementation.

## Context

ATHENA needs a forecasting model that staff can trust when deciding whether the
facility is trending toward a near-term capacity problem.

The first tracer bullet intentionally defers prediction so the platform can first
prove the presence and occupancy read path. The prediction design still needs to
be preserved before implementation starts so it is not lost.

## Decision

Use a lightweight, explainable prediction strategy based on:

- historical hourly binning
- recency weighting
- EWMA smoothing
- optional confidence bands
- holiday or anomalous-week exclusion when enough data exists

## Why This Approach

A facility with roughly a few hundred daily users does not justify a neural
network or a heavyweight time-series stack. EWMA with historical binning is:

- auditable
- explainable in interviews and operations reviews
- cheap to run
- easy to test against real occupancy history

This is the right tradeoff until real data proves otherwise.

## Implementation Notes

- aggregate `presence_events` into hourly counts by facility and later by zone
- weight more recent weeks higher than older weeks
- produce a near-term forecast window rather than broad long-range forecasting
- add confidence bands once enough historical variance exists to justify them

## Consequences

- the first implementation wave stays small
- prediction can be added later without changing the core presence model
- the system keeps a defensible, non-hyped forecasting story
