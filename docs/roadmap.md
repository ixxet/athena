# athena Roadmap

## Objective

Turn the platform from architecture into reality by shipping one clean end-to-end facility data slice.

## First Implementation Slice

- wire ATHENA to the minimum contracts from `ashton-proto`
- implement a mock adapter for presence events
- keep presence updates separate from matchmaking intent
- expose current occupancy through one read endpoint
- expose the same state through one CLI command
- publish at least one meaningful Prometheus metric

## Boundaries

- no real tap-in integration yet
- no broad predictive engine on day one
- no PWA or advanced dashboards until the core read path is stable

## Exit Criteria

- a mocked occupancy flow works end to end
- the repo has one stable read API surface
- the CLI confirms the same data as the API
- the service exposes metrics that Prometheus can scrape later

## Current State

Tracer 1 now has a stable narrow read slice:

- occupancy math is unit-tested across clamp, empty, unknown-facility, and multi-facility cases
- CLI, HTTP, and Prometheus all read through the same default-filtered occupancy path
- config validation and deterministic mock fixtures are in place before widening toward real adapters
- Tracer 2 now publishes only identified arrival events, emits bytes from the
  shared `ashton-proto` runtime contract, and keeps anonymous arrivals and
  departures out of the APOLLO visit path

## Tracer Ownership

- `Tracer 1`: mock presence -> API -> CLI -> metric
- `Tracer 2`: identified ATHENA arrival event -> APOLLO visit recording
