# Facility Truth Runbook

Tracer 18 adds a narrow ATHENA-owned facility-truth read model.

The source is file-backed and read-only. ATHENA does not infer facility truth
from occupancy, dormant Postgres schema files, or downstream product logic.

## What Is Real

- `athena facility list`
- `athena facility show --facility <id>`
- `GET /api/v1/facilities`
- `GET /api/v1/facilities/{facility_id}`

All four surfaces read the same validated catalog file.

## Catalog Shape

Set `ATHENA_FACILITY_CATALOG_PATH` to a JSON file with this structure:

```json
{
  "facilities": [
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}
      ],
      "zones": [
        {"zone_id": "gym-floor", "name": "Gym Floor"}
      ],
      "closure_windows": [
        {
          "starts_at": "2026-07-01T08:00:00-04:00",
          "ends_at": "2026-07-01T12:00:00-04:00",
          "code": "maintenance",
          "reason": "Morning maintenance",
          "zone_ids": ["gym-floor"]
        }
      ],
      "metadata": {
        "ingress_mode": "touchnet",
        "surface": "internal-only"
      }
    }
  ]
}
```

Validation rules:

- `facility_id` and `zone_id` must use lowercase letters, digits, and hyphens.
- every facility must declare `name`, `timezone`, `hours`, `zones`, and non-empty `metadata`
- hours are recurring weekly local-time windows and must not overlap inside a day
- closure windows are absolute RFC3339 intervals and must not conflict on the same scope
- any closure `zone_ids` must refer to declared facility zones

## Local Smoke

Use the checked-in fixture for bounded local proof:

```bash
ATHENA_FACILITY_CATALOG_PATH=docs/runbooks/fixtures/facility-catalog.json \
go run ./cmd/athena facility list --format json

ATHENA_FACILITY_CATALOG_PATH=docs/runbooks/fixtures/facility-catalog.json \
go run ./cmd/athena facility show --facility ashtonbee --format json
```

To exercise the internal HTTP surface:

```bash
ATHENA_FACILITY_CATALOG_PATH=docs/runbooks/fixtures/facility-catalog.json \
go run ./cmd/athena serve
```

Then, from another shell:

```bash
curl -sS http://127.0.0.1:8080/api/v1/facilities | jq
curl -sS http://127.0.0.1:8080/api/v1/facilities/ashtonbee | jq
```

## Negative Checks

The catalog should fail fast when:

- `ATHENA_FACILITY_CATALOG_PATH` is missing
- a facility is missing metadata
- hours windows overlap or reverse
- a closure references an unknown zone
- closure windows conflict on the same facility or zone scope

## Boundary Notes

- this is internal/operator truth, not a public product API
- ATHENA does not answer derived scheduling questions like `is_open_now`
- ATHENA does not publish the facility catalog over `ashton-proto`
- deployed truth remains unchanged unless deployment work is separately proven
