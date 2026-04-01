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
