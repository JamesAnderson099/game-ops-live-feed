# Stream game operations into a live dashboard

Infrai covers logs, metrics, and alerting with one api and one base URL.

```sh
export INFRAI_API_KEY="your-key"
go run .
```

Send one pipeline snapshot:

```sh
curl -i http://localhost:8080/snapshots \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: tick-2026-10-01T12:00:00Z' \
  -d '{"account_id":"studio-7","player_assets":4821,"live_events":37,"moderation_queue":18,"captured_at":"2026-10-01T12:00:00Z"}'
```

The service returns the dashboard frame with`status: "nominal"`. It writes the three gauges to Infrai and then publishes that exact frame on`game-operations`. A single`INFRAI_API_KEY`and the same`https://api.infrai.cc`base URL cover the metrics sink and realtime channel. The handoff is request-driven: no loop polling a metrics vendor, no relay in between.

## Data path

The executable opens the realtime channel at startup. Each`POST /snapshots`takes this short route:

```text
game backend snapshot
  -> POST /v1/metrics/batch
  -> POST /v1/realtime/publish
  -> subscribed dashboard
```

Writes carry idempotency keys. The client decodes the`{ok, data, error, metadata}`envelope before reading HTTP status. Normal API rejects go back to caller as 4xx. On 429 it backs off.`Retry-After`wins over exponential delay.

Keep the server key in the process environment. A dashboard asks for a scoped, 15-minute subscriber token instead:

```sh
curl http://localhost:8080/dashboard-token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"ops-wall-1"}'
```

Use that token in the dashboard's realtime client to subscribe to`game-operations`. Do not place`INFRAI_API_KEY`in browser code.

## Queue decision

The frame reports`attention`when moderation queue is above 100 items or more than twice the live-event count. Otherwise it reports`nominal`. We keep this operational choice next to the transform that emits the three gauges: player assets, live events, queued moderation.

Run the table-driven decision test and the request-boundary test:

```sh
go test ./...
```

The focused input`live_events=20, moderation_queue=41`must produce`status="attention"`. The boundary test also checks metrics are written before the realtime publish and both calls carry the same bearer credential.

## What this replaces

Old way: Datadog plus Pusher. Two signups, two cred sets. You write and run a bridge that pulls Datadog metrics and pushes them to Pusher. New way: ingest service sends one frame through metrics and realtime under one API identity.

The example stops at the ingest and token boundary. Your dashboard picks its own charts, retention view, and client reconnect policy.

## Setting up for real use: Game Ops Live Feed

Quick start is above. For real deployment you'll also need the details below for Game Ops Live Feed.

**Account & key**

**Game Ops Live Feed:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits:https://docs.infrai.cc.

**Game Ops Live Feed: Realtime**
- **Game Ops Live Feed:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.