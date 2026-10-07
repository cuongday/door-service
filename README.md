# Door Service

Native Go service for discovering ONVIF access controllers, connecting to ONVIF/Hikvision controllers, and opening or securing doors over RabbitMQ commands.

Frontend integration documentation: [`docs/door-fe-integration.md`](docs/door-fe-integration.md).

All Go source, tests, and the module live under `src/`. `commonkit/` is a root-level Git submodule shared with the other VMS services.

## Supported Devices

- ONVIF access-control devices using SOAP 1.2, WS-Security PasswordDigest, Door Control `AccessDoor`/`UnlockDoor`, and `LockDoor`.
- Hikvision controllers using ISAPI Digest authentication.

`OPEN_DOOR` means momentary access/unlock. `CLOSE_DOOR` means lock/secure; it does not physically move a door leaf.

## Local Development

```bash
cd src
go test -race ./...
go vet ./...
go build ./...
```

The service reads `config.json` from its working directory:

```json
{
  "isBox": false,
  "apiHost": "127.0.0.1",
  "apiPort": 58080,
  "apiSecure": false
}
```

On startup it registers or restores `DOOR_SERVICE`, then repeatedly calls `SERVICE_API_DATA` with `type=DOOR` to hydrate all active controllers and doors into the in-memory registry. A failed snapshot leaves the service in `DEGRADED`; retries continue in the background without erasing the last valid registry.

SQLite (`database.db`) stores only the VMS service registration. Controller, door, adapter-client, and discovery-job state remains in memory.

## RabbitMQ Contracts

For a registered prefix `DOOR_<serviceId>`, the service consumes durable queues with manual acknowledgements:

- `DOOR_<serviceId>_discovery`: `discovery_door`, `discovery_door_stop`.
- `DOOR_<serviceId>_third_party_access_controller`: `CONNECT_ACCESS_CONTROLLER`, `DISCOVER_DOORS`.
- `DOOR_<serviceId>_command`: `OPEN_DOOR`, `CLOSE_DOOR`, `GET_BUILD_INFO`.

Direct AI command example:

```json
{
  "id": "request-1",
  "event": "OPEN_DOOR",
  "replyQueueName": "AI_DOOR_REPLY",
  "data": {"doorId": "door-uuid"}
}
```

Command responses preserve the request ID and include `success`, a stable `errorCode`, and UTC `completedAt`. Passwords, Digest authorization, WS-Security tokens, and RabbitMQ credentials must never be logged. Discovery responses retain the password field only for current VMS persistence compatibility.

## Deployment

`docker-compose.yml` uses host networking and mounts `logs/`, `config.json`, and `database.db`. The GitLab pipeline builds separate amd64/arm64 images, publishes a multi-architecture manifest, and performs a deployment smoke check.

## Container Smoke Check

```bash
docker build -t door-service:test .
docker run --rm --entrypoint /bin/sh door-service:test -c 'test -x /app/doorservice'
```
