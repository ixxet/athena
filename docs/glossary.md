# ATHENA Glossary

## Purpose

Short definitions for the terms that appear repeatedly in ATHENA docs.

## Terms

| Term | Meaning in ATHENA |
| --- | --- |
| `physical truth` | What actually happened in the facility, independent of member intent or staff workflow |
| `presence` | Facility-entry or facility-exit state, not matchmaking or workout intent |
| `ingress adapter` | The component that reads raw upstream source data and converts it into ATHENA's internal presence model |
| `source-backed` | Derived from a non-mock upstream feed such as the current CSV export line |
| `canonical read path` | The single occupancy logic path shared by CLI, HTTP, and Prometheus |
| `identified presence` | A presence event tied to a stable identifier hash that can be used downstream by APOLLO |
| `visit-lifecycle publication` | The bounded event flow where ATHENA publishes identified arrival and departure subjects to NATS |
| `Tracer` | One narrow implementation slice that widens the stack only as far as the current proof requires |
