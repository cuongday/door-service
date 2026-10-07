package rbmqhandler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"doorservice/dto"
	serviceManager "doorservice/manager"
	"doorservice/store"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestCommandHandlerPreservesRequestIdentityAndReplyQueue(t *testing.T) {
	publisher := &messageCapture{}
	handler := NewCommandRabbitMQHandler(NewBaseHandler(publisher), fakeCommandExecutor{})
	delivery := deliveryFor(t, RBMQMessage{
		ID:             "req-open",
		Event:          EventOpenDoor,
		ReplyQueueName: "AI_REPLY",
		Data:           json.RawMessage(`{"doorId":"door-1"}`),
	})

	require.True(t, handler.Handle(delivery))
	published := publisher.Messages()
	require.Len(t, published, 1)
	require.Equal(t, "AI_REPLY", published[0].queue)
	require.Equal(t, "req-open", published[0].message.ID)
	require.Equal(t, EventOpenDoor, published[0].message.Event)
}

func TestDiscoveryHandlerPublishesResultAndCompatibilityDone(t *testing.T) {
	publisher := &messageCapture{}
	discovery := &fakeDiscoveryService{}
	handler := NewDiscoveryRabbitMQHandler(NewBaseHandler(publisher), discovery)
	delivery := deliveryFor(t, RBMQMessage{
		ID:             "req-discovery",
		Event:          EventDiscoveryDoor,
		ReplyQueueName: "VMS_REPLY",
		Data:           json.RawMessage(`{"aiBoxId":"00000000-0000-0000-0000-000000000552","fromIp":"192.168.1.10","toIp":"192.168.1.10","ports":[80]}`),
	})

	require.True(t, handler.Handle(delivery))
	published := publisher.Messages()
	require.Len(t, published, 2)
	require.Equal(t, "00000000-0000-0000-0000-000000000552", discovery.input.AIBoxID)
	require.Equal(t, EventDiscoveryDoor, published[0].message.Event)
	require.JSONEq(t, `{"aiBoxId":"00000000-0000-0000-0000-000000000552","ip":"192.168.1.10","port":80,"onvif_port":80,"state":true,"result_level":"success"}`, string(published[0].message.Data))
	require.JSONEq(t, `"Done"`, string(published[1].message.Data))
}

func TestAccessControllerHandlerPublishesDoorThenDone(t *testing.T) {
	publisher := &messageCapture{}
	adapter := &handlerAdapter{}
	handler := NewAccessControllerRabbitMQHandler(
		NewBaseHandler(publisher),
		staticHandlerFactory{adapter: adapter},
		nil,
		nil,
		nil,
	)
	delivery := deliveryFor(t, RBMQMessage{
		ID:             "req-controller",
		Event:          EventDiscoverDoors,
		ReplyQueueName: "VMS_REPLY",
		Data:           json.RawMessage(`{"id":"controller-1","type":"HIKVISION"}`),
	})

	require.True(t, handler.Handle(delivery))
	published := publisher.Messages()
	require.Len(t, published, 2)
	require.Equal(t, EventDiscovery, published[0].message.Event)
	require.Equal(t, EventDone, published[1].message.Event)
}

func TestConnectWithoutIDRepliesConnectedForVmsToStore(t *testing.T) {
	publisher := &messageCapture{}
	controllers := store.NewControllerStore()
	handler := NewAccessControllerRabbitMQHandler(
		NewBaseHandler(publisher), staticHandlerFactory{adapter: &handlerAdapter{}}, nil, controllers, nil,
	)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-connect", Event: EventConnectController, ReplyQueueName: "VMS_REPLY",
		Data: json.RawMessage(`{"type":"HIKVISION","ipAddress":"192.168.1.50","port":80}`),
	})

	require.True(t, handler.Handle(delivery))
	published := publisher.Messages()
	require.Len(t, published, 1)
	require.Equal(t, EventConnected, published[0].message.Event)
	require.Empty(t, controllers.GetAll())
}

func TestReconnectRefreshesStoredControllerCredentials(t *testing.T) {
	publisher := &messageCapture{}
	controllers := store.NewControllerStore()
	require.NoError(t, controllers.Upsert(dto.AccessControllerDTO{
		ID: "c1", Type: "HIKVISION", IPAddress: "192.168.1.50", Port: 80, Password: "old",
	}))
	handler := NewAccessControllerRabbitMQHandler(
		NewBaseHandler(publisher), staticHandlerFactory{adapter: &handlerAdapter{}}, nil, controllers, nil,
	)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-reconnect", Event: EventConnectController, ReplyQueueName: "VMS_REPLY",
		Data: json.RawMessage(`{"type":"HIKVISION","ipAddress":"192.168.1.50","port":80,"password":"new","tls":true}`),
	})

	require.True(t, handler.Handle(delivery))
	got, ok := controllers.Get("c1")
	require.True(t, ok)
	require.Equal(t, "new", got.Password)
	require.True(t, got.TLS)
	var reply dto.AccessControllerDTO
	require.NoError(t, json.Unmarshal(publisher.Messages()[0].message.Data, &reply))
	require.Equal(t, "c1", reply.ID)
}

func TestDiscoverDoorsWithoutControllerIDIsRejected(t *testing.T) {
	publisher := &messageCapture{}
	handler := NewAccessControllerRabbitMQHandler(
		NewBaseHandler(publisher), staticHandlerFactory{adapter: &handlerAdapter{}}, nil, nil, nil,
	)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-discover", Event: EventDiscoverDoors, ReplyQueueName: "VMS_REPLY",
		Data: json.RawMessage(`{"type":"HIKVISION","ipAddress":"192.168.1.50","port":80}`),
	})

	require.True(t, handler.Handle(delivery))
	require.Equal(t, EventError, publisher.Messages()[0].message.Event)
}

func TestDiscoveredDoorsAreNotRegisteredUntilVmsCreatesThem(t *testing.T) {
	publisher := &messageCapture{}
	doors := store.NewDoorStore()
	handler := NewAccessControllerRabbitMQHandler(
		NewBaseHandler(publisher), staticHandlerFactory{adapter: &handlerAdapter{}}, nil, nil, doors,
	)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-discover", Event: EventDiscoverDoors, ReplyQueueName: "VMS_REPLY",
		Data: json.RawMessage(`{"id":"controller-1","type":"HIKVISION"}`),
	})

	require.True(t, handler.Handle(delivery))
	require.Empty(t, doors.GetAll())
}

func TestDoorCreateReplacesEntryForSamePhysicalDoor(t *testing.T) {
	doors := store.NewDoorStore()
	index := 1
	require.NoError(t, doors.Upsert(dto.DoorDTO{ID: "c1:1", AccessControllerID: "c1", DoorIndex: &index}))
	handler := NewDoorSyncRabbitMQHandler(NewBaseHandler(&messageCapture{}), doors)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-door-create", Event: EventDoorCreate,
		Data: json.RawMessage(`{"id":"0b6f2a4e-0000-0000-0000-000000000001","accessControllerId":"c1","doorIndex":1}`),
	})

	require.True(t, handler.Handle(delivery))
	all := doors.GetAll()
	require.Len(t, all, 1)
	require.Equal(t, "0b6f2a4e-0000-0000-0000-000000000001", all[0].ID)
}

func TestDoorSyncHandlerCreatePreservesProtocolMetadata(t *testing.T) {
	doors := store.NewDoorStore()
	require.NoError(t, doors.Upsert(dto.DoorDTO{
		ID: "door-1", Name: "Old", IPAddress: "192.168.1.20",
		ONVIFEndpoint: "http://panel/onvif/door_control", ONVIFDoorToken: "door-token",
		ONVIFLockToken: "lock-token", ProtocolMetadata: map[string]any{"supportsAccessDoor": true},
	}))
	handler := NewDoorSyncRabbitMQHandler(NewBaseHandler(&messageCapture{}), doors)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-door-create", Event: EventDoorCreate,
		Data: json.RawMessage(`{"id":"door-1","name":"Lobby","ipAddress":"192.168.1.21"}`),
	})

	require.True(t, handler.Handle(delivery))
	got, ok := doors.Get("door-1")
	require.True(t, ok)
	require.Equal(t, "Lobby", got.Name)
	require.Equal(t, "192.168.1.21", got.IPAddress)
	require.Equal(t, "http://panel/onvif/door_control", got.ONVIFEndpoint)
	require.Equal(t, "door-token", got.ONVIFDoorToken)
	require.Equal(t, "lock-token", got.ONVIFLockToken)
	require.Equal(t, true, got.ProtocolMetadata["supportsAccessDoor"])
}

func TestDoorSyncHandlerUpdateChangesOnlyName(t *testing.T) {
	doors := store.NewDoorStore()
	require.NoError(t, doors.Upsert(dto.DoorDTO{
		ID: "door-2", Name: "Old", IPAddress: "192.168.1.30", Port: 80,
		ONVIFDoorToken: "token-2",
	}))
	handler := NewDoorSyncRabbitMQHandler(NewBaseHandler(&messageCapture{}), doors)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-door-update", Event: EventDoorUpdate,
		Data: json.RawMessage(`{"id":"door-2","name":"Renamed","ipAddress":"10.0.0.99","port":9000}`),
	})

	require.True(t, handler.Handle(delivery))
	got, _ := doors.Get("door-2")
	require.Equal(t, "Renamed", got.Name)
	require.Equal(t, "192.168.1.30", got.IPAddress)
	require.Equal(t, 80, got.Port)
	require.Equal(t, "token-2", got.ONVIFDoorToken)
}

func TestDoorSyncHandlerUpdateMissingDoorUpsertsSnapshot(t *testing.T) {
	doors := store.NewDoorStore()
	handler := NewDoorSyncRabbitMQHandler(NewBaseHandler(&messageCapture{}), doors)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-door-update-missing", Event: EventDoorUpdate,
		Data: json.RawMessage(`{"id":"door-3","name":"Recovered","ipAddress":"192.168.1.40"}`),
	})

	require.True(t, handler.Handle(delivery))
	got, ok := doors.Get("door-3")
	require.True(t, ok)
	require.Equal(t, "Recovered", got.Name)
	require.Equal(t, "192.168.1.40", got.IPAddress)
}

func TestDoorSyncHandlerDeleteIsIdempotent(t *testing.T) {
	doors := store.NewDoorStore()
	require.NoError(t, doors.Upsert(dto.DoorDTO{ID: "door-4", Name: "Delete me"}))
	handler := NewDoorSyncRabbitMQHandler(NewBaseHandler(&messageCapture{}), doors)
	delivery := deliveryFor(t, RBMQMessage{
		ID: "req-door-delete", Event: EventDoorDelete,
		Data: json.RawMessage(`{"id":"door-4"}`),
	})

	require.True(t, handler.Handle(delivery))
	require.True(t, handler.Handle(delivery))
	_, ok := doors.Get("door-4")
	require.False(t, ok)
}

func deliveryFor(t *testing.T, message RBMQMessage) amqp091.Delivery {
	t.Helper()
	body, err := json.Marshal(message)
	require.NoError(t, err)
	return amqp091.Delivery{Body: body}
}

type publishedMessage struct {
	queue   string
	message RBMQMessage
}

type messageCapture struct {
	mu       sync.Mutex
	messages []publishedMessage
}

func (c *messageCapture) Publish(queue string, message RBMQMessage) error {
	c.mu.Lock()
	c.messages = append(c.messages, publishedMessage{queue: queue, message: message})
	c.mu.Unlock()
	return nil
}

func (c *messageCapture) Messages() []publishedMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]publishedMessage(nil), c.messages...)
}

type fakeCommandExecutor struct{}

func (fakeCommandExecutor) ExecuteRequest(_ context.Context, requestID string, command dto.DoorCommand) dto.CommandResult {
	return dto.CommandResult{RequestID: requestID, DoorID: command.DoorID, Command: command.Command, Success: true}
}

type fakeDiscoveryService struct {
	input dto.DiscoveryDoorInfoDTO
}

func (f *fakeDiscoveryService) Start(
	_ context.Context,
	_ string,
	input dto.DiscoveryDoorInfoDTO,
	onResult func(dto.DiscoveryResult),
	onDone func(),
) error {
	f.input = input
	onResult(dto.DiscoveryResult{AIBoxID: input.AIBoxID, IP: "192.168.1.10", Port: 80, ONVIFPort: 80, State: true, ResultLevel: "success"})
	onDone()
	return nil
}

func (*fakeDiscoveryService) Stop(string) bool { return true }

type staticHandlerFactory struct {
	adapter serviceManager.DoorAdapter
}

func (f staticHandlerFactory) ForDoor(dto.DoorDTO) (serviceManager.DoorAdapter, error) {
	return f.adapter, nil
}

func (f staticHandlerFactory) ForController(dto.AccessControllerDTO) (serviceManager.DoorAdapter, error) {
	return f.adapter, nil
}

type handlerAdapter struct{}

func (*handlerAdapter) Connect(_ context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	return controller, nil
}

func (*handlerAdapter) DiscoverDoors(_ context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	return []dto.DoorDTO{{ID: "door-1", AccessControllerID: controller.ID}}, nil
}

func (*handlerAdapter) OpenDoor(context.Context, dto.DoorDTO) error  { return nil }
func (*handlerAdapter) CloseDoor(context.Context, dto.DoorDTO) error { return nil }
