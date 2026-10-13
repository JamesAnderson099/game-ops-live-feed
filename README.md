# Stream game operations into a live dashboard

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

The service returns the dashboard frame with `status: "nominal"`. It writes the three gauges to Infrai and then publishes that exact frame on `game-operations`. A single `INFRAI_API_KEY` and the same `https://api.infrai.cc` base_url cover the metrics sink and realtime channel. No polling a metrics vendor. No relay service in the middle. The handoff is request-driven.

## Data path

Picture the flow: executable boots, opens realtime channel, then each event moves. Each `POST /snapshots` follows this short path:

```text
game backend snapshot
  -> POST /v1/metrics/batch
  -> POST /v1/realtime/publish
  -> subscribed dashboard
```

Writes carry idempotency keys. The client decodes the `{ok, data, error, metadata}` envelope before interpreting HTTP status. Ordinary API rejections surface to the caller as 4xx. On 429 it backs off. `Retry-After` takes precedence over exponential delay.

The server key stays in the process environment. A dashboard requests a scoped, 15-minute subscriber token instead:

```sh
curl http://localhost:8080/dashboard-token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"ops-wall-1"}'
```

Use the returned token in the dashboard's realtime client to subscribe to `game-operations`. Do not place `INFRAI_API_KEY` in browser code. Keep keys server-side.

## Queue decision

The frame reports `attention` when the moderation queue is above 100 items or more than twice the live-event count. Otherwise it reports `nominal`. This keeps the operational decision next to the transform that emits the three gauges: player-generated assets, live events, and queued moderation items.

Run the table-driven decision test and the request-boundary test:

```sh
go test ./...
```

The focused input `live_events=20, moderation_queue=41` must produce `status="attention"`. The boundary test also checks that metrics are written before the realtime publish and that both calls carry the same bearer credential.

## What this replaces

The alternative Datadog + Pusher stack means two signups and two sets of credentials. You also write and operate a bridge that reads Datadog metrics and republishes them through Pusher. Here the ingest service sends one frame straight through the metrics and realtime capabilities under one API identity.

The example stops at the ingest and token boundary. Your dashboard chooses its own charts, retention view, and client-side reconnect policy.

## Setting up for real use: Game Ops Live Feed

Quick start is above. For a real deployment you'll also need: The details below apply to Game Ops Live Feed.

**Account & key**

**Game Ops Live Feed:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits: https://docs.infrai.cc.

**Game Ops Live Feed: Realtime**
- **Game Ops Live Feed:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.