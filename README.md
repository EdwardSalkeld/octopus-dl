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

With no arguments, it downloads yesterday's electricity and gas consumption and
upserts it into the `usages` table:

```sh
./octopus-dl
```

### Backfill

To re-fetch a historical window (chunked into 48h requests), pass UTC
timestamps:

```sh
./octopus-dl -backfill-from 2026-04-10T00:00:00Z -backfill-to 2026-04-20T00:00:00Z -backfill-type both
```

`-backfill-type` accepts `electricity`, `gas`, or `both` (default `both`).

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
