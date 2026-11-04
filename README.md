# octopus-dl

A one-shot downloader for Octopus Energy electricity and gas consumption data.
It fetches half-hourly consumption from the Octopus API and upserts it into a
PostgreSQL `usages` table.

This was lifted out of the `scheduler` service so it can be run as a plain
cron-style one-shot job (e.g. a systemd timer on NixOS) instead of a long-lived
container.

## Usage

Configure via a `.env` file (see `.env.example`) or the process environment:

| Variable          | Description                                  | Default     |
| ----------------- | -------------------------------------------- | ----------- |
| `DB_HOST`         | PostgreSQL host (or unix socket directory)   | `localhost` |
| `DB_PORT`         | PostgreSQL port                              | `5432`      |
| `DB_USER`         | PostgreSQL user                              |             |
| `DB_PASSWORD`     | PostgreSQL password (omit for peer auth)     |             |
| `DB_NAME`         | PostgreSQL database                          |             |
| `DB_SSLMODE`      | PostgreSQL SSL mode                          | `disable`   |
| `OCTOPUS_API_KEY` | Octopus Energy API key                       |             |

### Daily download (default)

With no arguments, it re-fetches the preceding 14 UTC calendar days of electricity
and gas consumption, ending at today's UTC midnight, and upserts them into
the `usages` table:

```sh
./octopus-dl
```

Readings may arrive several days after the initial pull. A rolling reconciliation
window picks them up on a later daily run instead of leaving permanent gaps once
a date falls outside the former 48-hour window. Existing rows are updated if
Octopus corrects consumption; empty responses never delete stored readings.

The 14-day range uses the existing sequential 48-hour requests: seven per fuel,
14 total per daily run. It is not one large request that would exceed the API's
usual first-page limit. Response auditing, per-request timeouts/retries and the
shared advisory lock also apply to reconciliation. A failed chunk stops the run;
the next run retries the overlapping date range. This does not promise that
Octopus has already published every interval. Gaps or corrections older than
14 days still require an explicit historical backfill.

### Backfill

To re-fetch a historical window (chunked into 48h requests), pass UTC
timestamps:

```sh
./octopus-dl -backfill-from 2026-04-10T00:00:00Z -backfill-to 2026-04-20T00:00:00Z -backfill-type both
```

`-backfill-type` accepts `electricity`, `gas`, or `both` (default `both`).

### Manual trigger endpoint

Pass `-listen-addr` to keep the downloader running as a small HTTP service.
`POST /run` with an empty body performs one normal daily download and returns
only after both meter requests have finished. To re-fetch a historical window,
send JSON with RFC3339 UTC bounds and an optional usage type (default `both`):

```sh
curl --fail-with-body -X POST http://localhost:8790/run \
  -H 'Content-Type: application/json' \
  --data '{"period_from":"2026-08-24T00:00:00Z","period_to":"2026-08-31T00:00:00Z","usage_type":"gas"}'
```

`usage_type` accepts `electricity`, `gas`, or `both`. The request is rejected
with `400 Bad Request` unless both period bounds are supplied, valid, and
ordered. Concurrent requests return `409 Conflict`, so a manual trigger cannot
overlap another manual trigger. `GET /healthz` returns `204 No Content`.

The endpoint has no built-in authentication and must be bound to loopback or
protected by a reverse proxy. The NixOS deployment exposes it only to the
Magpie worker over the tailnet.

## Database

The `usages` table is expected to already exist:

```sql
CREATE TABLE IF NOT EXISTS usages (
  consumption double precision NOT NULL,
  interval_start timestamptz NOT NULL,
  interval_end timestamptz NOT NULL,
  usage_type text NOT NULL,
  PRIMARY KEY (interval_start, usage_type)
);
```

## Build and test

```sh
go test ./...
go build -o octopus-dl .
```

## Docker

```sh
docker build -t octopus-dl .
docker run --rm --env-file .env octopus-dl
```
