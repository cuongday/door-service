# Door CRUD Synchronization Design

## Objective

Synchronize every persisted Door create, rename, and delete event from `vms-be` to three downstream consumers:

1. The scoped `door-service` instance over RabbitMQ.
2. Kafka on a dedicated door topic.
3. Connected VMS frontend clients over websocket.

Door updates are intentionally restricted to rename only. The existing generic `PUT /api/door` and `PATCH /api/door` endpoints will be removed and replaced with `PATCH /api/door/rename`.

## Scope

### Included

- Manual door creation through `POST /api/door`.
- Adding discovered doors through `POST /api/door/discovery/add`.
- Renaming a door through `PATCH /api/door/rename`.
- Door deletion through `DELETE /api/door`.
- RabbitMQ synchronization to the appropriate `door-service` instance.
- Kafka synchronization using a new `doorTopicName` configuration.
- User websocket notifications.
- A new door CRUD consumer and store mutation support in `door-service`.
- Tests and FE integration documentation updates.

### Excluded

- Updating door fields other than `name`.
- Kafka consumption inside `door-service`.
- Delivery acknowledgements from `door-service` back to `vms-be` for CRUD sync.
- Retrying failed Kafka or websocket delivery beyond the existing client behavior.
- Changing door discovery or door command contracts.

## API Contract

### Create

`POST /api/door` remains unchanged. A successfully saved door is synchronized with event `DOOR_CREATE`.

### Add Discovered Doors

`POST /api/door/discovery/add` remains unchanged. Each selected door is marked `added=true`, saved, and synchronized with event `DOOR_CREATE`.

Discovery results with `added=false` are not synchronized as created doors until this endpoint is called.

### Rename

Remove:

- `PUT /api/door`
- `PATCH /api/door`

Add:

```http
PATCH /api/door/rename
Content-Type: application/json
```

Example payload:

```json
{
  "id": "a37b9fd0-44b9-4ef0-88df-394999bf01f2",
  "name": "Lobby Entrance",
  "activate": false,
  "ipAddress": "10.0.0.10"
}
```

Only `id` and `name` are read. Additional fields are ignored. The response is the updated `DoorDTO`. A successful rename is synchronized with event `DOOR_UPDATE`.

The rename request must contain a valid door ID and a non-blank name. A missing door returns the existing not-found error style; invalid input returns the existing `INVALID` error style.

### Delete

`DELETE /api/door?ids=...` remains unchanged. The service captures each `DoorDTO`, emits `DOOR_DELETE`, then removes the entities.

## Event Contract

Add the following constants to `CRUDEvent`:

```text
DOOR_CREATE
DOOR_UPDATE
DOOR_DELETE
```

All three transports use the same event value and a full `DoorDTO` snapshot as data.

### Kafka

Add to `AppProperties.Kafka`:

```java
private String doorTopicName;
```

Add to `application.yml`:

```yaml
company:
  kafka:
    doorTopicName: ${DOOR_TOPIC}
```

Tests use `test-door` in `application-test.yml`. Deployment environments must provide `DOOR_TOPIC`. The unresolved-conflict `.env` file is not modified by this change.

Kafka payload:

```json
{
  "id": "message-uuid",
  "createdAt": "timestamp",
  "event": "DOOR_UPDATE",
  "data": {
    "id": "door-uuid",
    "name": "Lobby Entrance"
  }
}
```

The Kafka topic is created through the same `KafkaService.createTopic` pattern used by Camera.

### Websocket

Map CRUD events to frontend websocket events:

| CRUD event | Websocket event |
|---|---|
| `DOOR_CREATE` | `vms_door_create` |
| `DOOR_UPDATE` | `vms_door_update` |
| `DOOR_DELETE` | `vms_door_delete` |

The websocket payload is a `WebsocketMessage` containing the full `DoorDTO` in `data`.

### RabbitMQ to door-service

Resolve the destination `door-service` using the door's effective AI-Box scope:

- AI-Box door: `findFirstConnectedByScopeAndType(aiBoxId, DOOR)`.
- Server door: `findFirstConnectedByScopeAndType(null, DOOR)`.

Destination queue:

```text
DOOR_<serviceId>_door
```

RabbitMQ message:

```json
{
  "event": "DOOR_UPDATE",
  "dst": "DOOR_<serviceId>_door",
  "data": {
    "id": "door-uuid",
    "name": "Lobby Entrance"
  }
}
```

If no connected scoped DOOR service exists, VMS logs a warning and still sends Kafka and websocket events.

## VMS Backend Design

### Door sync method

Implement transport fan-out directly in `DoorService.syncDoor`. `DoorService` receives these additional dependencies:

- `ServiceService`
- `RabbitMQService`
- `KafkaService`
- `UserWebsocketHandler`
- `MapperService`
- `AppProperties`

`DoorService.syncDoor(Door door, String event)` converts the entity to `DoorDTO` and performs RabbitMQ, Kafka, and websocket delivery itself. An overload accepting `DoorDTO` may be used by delete so the snapshot remains available after entity removal.

Each transport is isolated in its own `try/catch`. Synchronization is best-effort, matching Camera behavior: a downstream failure is logged and does not roll back a successful database mutation.

### CRUD integration points

- Override `DoorService.create(DoorDTO)` to call `super.create`, then `syncDoor(..., DOOR_CREATE)`.
- In `addDiscoveredDoor`, call `syncDoor(..., DOOR_CREATE)` after each door becomes `added=true` and is saved.
- Add `DoorService.rename(DoorRenameDTO)` which loads the entity, changes only `name`, saves, and calls `syncDoor(..., DOOR_UPDATE)`.
- In `delete`, capture DTO snapshots, emit `DOOR_DELETE`, then call `repo.deleteAll`.

The existing `DoorService.findAll` behavior remains restricted to `added=true`, so delete continues to operate only on added doors.

## door-service Design

### Queue wiring

Extend `QueueNames` with:

```text
Door = <registered-service-prefix>_door
```

Create and start a fourth RabbitMQ consumer with manual acknowledgement, alongside discovery, access-controller, and command consumers.

### Door CRUD handler

Add `DoorSyncRabbitMQHandler` with these behaviors:

- `DOOR_CREATE`: deserialize `DoorDTO` and upsert the full door into `DoorStore`.
- `DOOR_UPDATE`: if the door exists, preserve all stored fields and replace only `Name`; if it does not exist, upsert the received full snapshot to self-heal after a missed create/bootstrap.
- `DOOR_DELETE`: remove the door by ID. Deleting a missing door is idempotent and succeeds.
- Any other event: reject as an invalid request.

No reply queue is required for CRUD synchronization.

### Store support

Add an idempotent `DoorStore.Delete(id string) bool` method. It removes the door and all secondary indexes under the existing store lock.

## Failure Handling

- Database validation/save failures stop the request and emit no synchronization event.
- RabbitMQ, Kafka, or websocket failures are logged independently and do not change the HTTP success response.
- A missing scoped door-service skips RabbitMQ only; Kafka and websocket still run.
- door-service rejects malformed payloads without mutating its store.
- Door delete is idempotent in door-service.

## Testing Strategy

### vms-be

- Create emits `DOOR_CREATE` after save.
- Adding a discovered door emits `DOOR_CREATE` only after `added=true` is persisted.
- Rename changes only `name` even when the payload contains other fields.
- Old PUT/PATCH `/api/door` routes are absent; PATCH `/api/door/rename` is present.
- Delete emits a full `DoorDTO` with `DOOR_DELETE` before repository deletion.
- `DoorService.syncDoor` routes by effective AI-Box scope.
- RabbitMQ destination is `DOOR_<serviceId>_door`.
- Kafka uses `doorTopicName` and a `CRUDMessage`.
- Websocket event names match the contract.
- Failure in one transport does not prevent attempts on the other transports.

### door-service

- Queue construction includes `<prefix>_door`.
- Create upserts a door.
- Update changes only the stored name.
- Update of a missing door self-heals by upserting the snapshot.
- Delete removes the door and its indexes.
- Delete of a missing door succeeds.
- Unsupported events and malformed payloads do not mutate the store.
- Focused Go tests pass and `go build ./...` succeeds.

## Documentation

Update the FE integration Markdown, DOCX, Postman collection, and Postman README:

- Replace PUT/PATCH update examples with PATCH `/api/door/rename`.
- State that only `id` and `name` are used.
- Document websocket event names.
- Document that create/add/rename/delete are synchronized asynchronously.
