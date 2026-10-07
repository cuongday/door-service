# Door Service Design

## Objective

Build a native Go service that discovers, registers, and controls ONVIF and Hikvision doors. The service owns a thread-safe in-memory registry so VMS and AI services can issue RabbitMQ commands by door ID without carrying device credentials in every request.

The first implementation phase builds `door-service` and the VMS bootstrap snapshot. The second phase redirects existing VMS door traffic from `restream_service` to `door-service` and removes the obsolete door code from restream after compatibility is verified.

## Project Shape

The service keeps the lifecycle and package boundaries of `onvifservice`, but all Go module code lives under one `src/` directory instead of at repository root or in a `cmd/internal` layout:

```text
door-service/
  src/
    api/
      api.go
    config/
      base_config.go
      box_config.go
      server_config.go
      config_loader.go
    db/
      service_model.go
      service_repo.go
    dto/
      access_controller_dto.go
      door_dto.go
      discovery_door_info_dto.go
      command_dto.go
    manager/
      manager.go
      onvifmanager/
      hikvisionmanager/
      discoverymanager/
    store/
      controller_store.go
      door_store.go
      client_store.go
      job_store.go
    rbmqhandler/
      rbmq_message.go
      event_type.go
      discovery_rbmq_handler.go
      access_controller_rbmq_handler.go
      command_rbmq_handler.go
    version/
      version.go
    websocket/
      client.go
    main.go
    go.mod
  commonkit/
  config.json
  Dockerfile
  docker-compose.yml
  .gitlab-ci.yml
  .gitmodules
```

`commonkit` is a root-level Git submodule, referenced from `src/go.mod` with `replace commonkit => ../commonkit`. The submodule URL must use the repository-relative GitLab form and must not contain embedded credentials. The service reuses commonkit RabbitMQ, logging, JSON, HTTP, network, SQLite, websocket, scheduling, and system-monitor packages.

Docker, multi-architecture CI, build metadata stamping, timezone configuration, and compose conventions mirror `onvifservice`, with names changed to `door-service`.

## Startup Lifecycle

1. Load box/server configuration through the same singleton config pattern as `onvifservice`.
2. Register `DOOR_SERVICE` with service type `DOOR` through the VMS service API.
3. Cache the registered service record in local SQLite using the commonkit database helper. This database stores service registration only; controller and door runtime state remains in memory.
4. Apply RabbitMQ credentials, queue prefix, queue postfix, and websocket URL returned by VMS.
5. Connect the websocket client so service configuration updates follow the same lifecycle as `onvifservice`.
6. Call `GET /api/service/data?id=<service-id>&type=DOOR` and hydrate the controller and door registries.
7. Start managers, RabbitMQ consumers and handlers, then the commonkit system monitor.

If VMS bootstrap is unavailable, the process remains running in `DEGRADED` state and retries with bounded exponential backoff. Connect and discovery messages may populate the registry while degraded. Door-ID commands return `REGISTRY_NOT_READY` or `DOOR_NOT_FOUND` until the required record exists.

## VMS Service Data Contract

`vms-be` adds service type/entity/DTO support for `DOOR`. `ServiceService.getData()` returns a `DoorServiceDTO` when the requested service has type `DOOR`.

Every door-service instance receives all active controllers and all active doors; there is no service-to-door assignment in the first version.

`DoorServiceDTO` extends `ServiceDTO` and contains:

- `accessControllers`: controller ID, controller type, brand, IP address, port, username, password, TLS settings, activation/state, and vendor-specific identity fields needed to reconnect.
- `doors`: door ID, door type, name, activation/state, access-controller ID, IP address, port, username, password, ONVIF endpoint and tokens, Hikvision door index, and protocol metadata needed to execute commands.

The payload uses the existing VMS DTO serialization and credential handling. Passwords must never be included in service logs. Controllers are loaded before doors so every door reference can be resolved during hydration. Invalid references are skipped and reported without aborting the entire snapshot.

## Runtime Registry

The registry has four thread-safe stores:

- `ControllerStore`: controller descriptors indexed by VMS UUID.
- `DoorStore`: door descriptors indexed by VMS UUID, with lookup by controller ID and protocol-native token/index.
- `ClientStore`: lazily created ONVIF or Hikvision clients indexed by a stable controller identity.
- `JobStore`: active discovery jobs indexed by RabbitMQ request ID.

Stores use `sync.RWMutex`. Client creation uses a per-controller mutex, following the `onvifservice` device cache pattern, so concurrent commands do not create duplicate clients. Controller and door records are upserted by bootstrap, connect, and discovery flows.

The VMS snapshot is the recovery source after restart. The in-memory registry is the command-serving source during runtime.

## Manager Interfaces

Protocol managers implement a shared behavior boundary:

```go
type DoorAdapter interface {
    Connect(ctx context.Context, controller Controller) (ControllerInfo, error)
    DiscoverDoors(ctx context.Context, controller Controller) ([]DiscoveredDoor, error)
    OpenDoor(ctx context.Context, door Door) error
    CloseDoor(ctx context.Context, door Door) error
}
```

An adapter factory selects ONVIF or Hikvision from the controller/door type. Application managers resolve registry records, obtain an adapter, execute the operation, update runtime state, and convert protocol failures into stable service errors.

`CloseDoor` means returning the lock to its secured/normal state. It does not imply physically moving the door leaf; physical closing is performed by the installed door closer.

## ONVIF Behavior

The Go implementation ports the functional pipeline from `door-scan`:

1. Expand individual IPs, ranges, and configured ONVIF ports.
2. Probe `/onvif/device_service`.
3. Try configured username/password combinations.
4. Read device information and service capabilities.
5. Detect Access Control and Door Control namespaces.
6. Read access-control inventory and a status snapshot.
7. Emit one result for each target and a final completion message.

SOAP requests use WS-Security UsernameToken headers and per-request timeouts. Successful discovery stores the device endpoint, credential, door token, lock token, service endpoints, and capability metadata.

Open uses the supported Door Control unlock/access operation. Close uses the supported lock/secure operation. A device that does not advertise the required capability returns `UNSUPPORTED_PROTOCOL` instead of attempting a vendor-specific request through the ONVIF adapter.

## Hikvision Behavior

The Hikvision adapter uses ISAPI with HTTP Digest authentication:

- Connect: `GET /ISAPI/System/deviceInfo`.
- Discover doors: `GET /ISAPI/AccessControl/Door/channels`.
- Open/close: the ISAPI remote door-control operation for the stored `doorIndex`.

The base URL uses the controller TLS setting. Certificate verification is configurable and defaults to the controller configuration. The default request timeout is 10 seconds. Connect and discovery responses retain manufacturer, model, serial number, firmware, door index, and door name.

## Discovery Execution

Discovery uses bounded Go worker pools rather than creating an unbounded goroutine per IP. Job state is controlled by `context.Context` cancellation and stored under the RabbitMQ request ID.

Each target produces exactly one terminal discovery result: success, partial, authentication failure, non-access device, unreachable, or cancelled. A job emits its final completion message after all started workers exit. Stop is idempotent and does not affect other discovery jobs.

## RabbitMQ Contracts

Messages retain the existing `RBMQMessage` envelope: `id`, `event`, `data`, `dst`, `replyQueueName`, and `username`.

### Discovery Queue

Queue: `DOOR_<service-id>_discovery`

- `discovery_door`: starts ONVIF door discovery using the door-specific `DiscoveryDoorInfoDTO` payload. It contains only `usernames`, `passwords`, `fromIp`, `toIp`, `ports`, and `onvifPort`; camera-only fields are not part of this contract.
- `discovery_door_stop`: cancels the job identified by the message ID.

Each result retains `event=discovery_door`. Completion retains the current compatibility value `data="Done"`.

### Third-Party Access Controller Queue

Queue: `DOOR_<service-id>_third_party_access_controller`

- `CONNECT_ACCESS_CONTROLLER`: validates a Hikvision controller and upserts it into the registry.
- `DISCOVER_DOORS`: discovers and upserts controller doors.

Discovery replies retain the current sequence of one or more `DISCOVERY` events followed by `DONE`. Failures use `ERROR` and preserve the request ID and reply queue.

### Command Queue

Queue: `DOOR_<service-id>_command`

- `OPEN_DOOR`
- `CLOSE_DOOR`
- existing operational command `GET_BUILD_INFO`

Door commands use `{ "doorId": "<uuid>" }` as the primary payload. The response includes request ID, door ID, command, success status, error code/message when applicable, and completion time. This queue is also the direct integration point for AI services; they provide a reply queue and do not call VMS.

RabbitMQ handlers follow the `onvifservice` base-handler and event-map pattern. Discovery, controller, and command handlers remain separate to keep responsibilities and test suites focused.

## Error Model

Stable service error codes are:

- `INVALID_REQUEST`
- `REGISTRY_NOT_READY`
- `DOOR_NOT_FOUND`
- `CONTROLLER_NOT_FOUND`
- `UNSUPPORTED_PROTOCOL`
- `AUTH_FAILED`
- `DEVICE_UNREACHABLE`
- `COMMAND_REJECTED`
- `COMMAND_TIMEOUT`
- `DISCOVERY_CANCELLED`
- `INTERNAL_ERROR`

Malformed messages are rejected without crashing a consumer. Valid messages preserve their request ID in success and error replies. Logs include controller/door IDs and elapsed time but redact usernames when unnecessary and always redact passwords, authorization headers, and WS-Security secrets.

## Concurrency and Delivery

- Registry reads use shared locks; mutations use exclusive locks.
- Each controller has a command mutex to prevent overlapping connect, discovery, open, and close operations against devices that do not support concurrent control calls.
- All protocol calls have context deadlines.
- Consumer acknowledgement follows commonkit behavior and occurs only after validation and safe handoff to the bounded worker path.
- Panic recovery at worker boundaries converts unexpected failures to `INTERNAL_ERROR` and leaves the consumer alive.

## Testing Strategy

Go tests cover:

- Registry CRUD, hydration order, invalid references, and concurrent access.
- Adapter selection, error mapping, lazy client caching, and per-controller locking.
- ONVIF WS-Security generation, SOAP parsing, service-capability detection, inventory, status, open, and secure operations using fixtures.
- Hikvision Digest Auth requests, device-info parsing, door discovery, and remote open/close requests using fixtures.
- IP/range expansion, credential fallback, bounded concurrency, cancellation, result classification, and final completion behavior.
- RabbitMQ compatibility for existing discovery and third-party events plus `OPEN_DOOR` and `CLOSE_DOOR`.
- VMS bootstrap success, partial invalid data, retry/backoff, and degraded operation using an HTTP test server.
- Race detection with `go test -race ./...`.

VMS tests cover:

- `DOOR` service subtype serialization and mapper behavior.
- Service creation/registration for type `DOOR`.
- `SERVICE_API_DATA` returning every active controller and door with correct relationships and excluding inactive records.
- Empty snapshots and missing-service/type-mismatch failures.

## Delivery Phases

### Phase 1: Door Service and Bootstrap Snapshot

- Scaffold `door-service` from the `onvifservice` structure.
- Add the `commonkit` submodule.
- Implement registration, config, websocket, bootstrap, registry, discovery, protocol adapters, RabbitMQ handlers, Docker, compose, and CI.
- Add VMS `DOOR` service support and `SERVICE_API_DATA` snapshot.
- Verify the native Go service independently while existing production routing still targets restream.

### Phase 2: Routing Migration

- Change VMS ONVIF door discovery destination from `RESTREAM_<id>_discovery` to `DOOR_<id>_discovery`.
- Change VMS third-party controller connect/discovery destination from `RESTREAM_<id>_third_party_access_controller` to the matching `DOOR` queue.
- Add VMS open/close command publishing and response listeners.
- Run compatibility tests against both protocols.
- Remove door discovery and Hikvision access-controller handling from `restream_service` only after the migrated paths pass.

## Out of Scope for the First Delivery

- HTTP control endpoints in `door-service`.
- Persistent controller/door storage inside `door-service` beyond the VMS recovery snapshot.
- Door-to-service sharding or assignment.
- Vendors other than ONVIF and Hikvision.
- Physical door-position control beyond the lock/relay capabilities exposed by the device.
