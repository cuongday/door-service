package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	commonrbmq "commonkit/rbmq"
	"commonkit/systemmonitor"
	"doorservice/api"
	"doorservice/config"
	"doorservice/manager/commandmanager"
	"doorservice/manager/dahuamanager"
	"doorservice/manager/discoverymanager"
	"doorservice/manager/hikvisionmanager"
	"doorservice/manager/onvifmanager"
	"doorservice/manager/registrymanager"
	"doorservice/manager/zktecomanager"
	"doorservice/rbmqhandler"
	"doorservice/store"
)

const configPath = "config.json"

type QueueNames struct {
	Discovery        string
	AccessController string
	Command          string
	Door             string
}

func BuildQueueNames(prefix string) QueueNames {
	return QueueNames{
		Discovery:        prefix + "_discovery",
		AccessController: prefix + "_third_party_access_controller",
		Command:          prefix + "_command",
		Door:             prefix + "_door",
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configPath); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("door-service stopped with error: %v", err)
	}
}

func run(ctx context.Context, path string) error {
	config.Load(path)
	serviceConfig := config.GetConfig()
	if err := serviceConfig.Start(ctx); err != nil {
		return err
	}
	base := serviceConfig.GetConfig()
	queues := BuildQueueNames(base.RbmqQueuePrefix)

	controllers := store.NewControllerStore()
	doors := store.NewDoorStore()
	clients := store.NewClientStore()
	jobs := store.NewJobStore()
	registry := registrymanager.New(controllers, doors)

	httpClient := newServiceHTTPClient(15 * time.Second)
	onvif := onvifmanager.New(httpClient)
	hikvision := hikvisionmanager.New(httpClient)
	zkteco := zktecomanager.New(httpClient)
	dahua := dahuamanager.New(httpClient)
	factory := commandmanager.NewFactory(onvif, hikvision, zkteco, dahua)
	discovery := discoverymanager.New(onvif, controllers, doors, jobs, 16)
	commands := commandmanager.New(
		doors,
		controllers,
		factory,
		commandmanager.WithClientStore(clients),
		commandmanager.WithRegistry(registry),
	)

	producer := rabbitBuilder(base).BuildProduer()
	publisher := rbmqhandler.NewCommonkitPublisher(producer)
	baseHandler := rbmqhandler.NewBaseHandler(publisher)
	discoveryHandler := rbmqhandler.NewDiscoveryRabbitMQHandler(baseHandler, discovery, ctx)
	controllerHandler := rbmqhandler.NewAccessControllerRabbitMQHandler(
		baseHandler,
		factory,
		clients,
		controllers,
		doors,
	)
	commandHandler := rbmqhandler.NewCommandRabbitMQHandler(baseHandler, commands)
	doorSyncHandler := rbmqhandler.NewDoorSyncRabbitMQHandler(baseHandler, doors)

	discoveryConsumer := rabbitBuilder(base).
		WithQueue(queues.Discovery, true).
		BuildConsumer().
		WithAutoAck(false).
		WithPrefetchCount(16)
	controllerConsumer := rabbitBuilder(base).
		WithQueue(queues.AccessController, true).
		BuildConsumer().
		WithAutoAck(false).
		WithPrefetchCount(8)
	commandConsumer := rabbitBuilder(base).
		WithQueue(queues.Command, true).
		BuildConsumer().
		WithAutoAck(false).
		WithPrefetchCount(30)
	doorSyncConsumer := rabbitBuilder(base).
		WithQueue(queues.Door, true).
		BuildConsumer().
		WithAutoAck(false).
		WithPrefetchCount(30)

	discoveryConsumer.RegisterHandler(discoveryHandler)
	controllerConsumer.RegisterHandler(controllerHandler)
	commandConsumer.RegisterHandler(commandHandler)
	doorSyncConsumer.RegisterHandler(doorSyncHandler)

	vmsAPI := api.New(base.VmsAPIURL, nil)
	go func() {
		_ = registry.Bootstrap(ctx, func(fetchCtx context.Context) (api.DoorServiceData, error) {
			data, err := vmsAPI.ReadDoorServiceData(base.ServiceID)
			if err != nil {
				return api.DoorServiceData{}, err
			}
			if data == nil {
				return api.DoorServiceData{}, errors.New("no data returned")
			}
			return *data, nil
		})
	}()

	producer.Start()
	discoveryConsumer.Start()
	controllerConsumer.Start()
	commandConsumer.Start()
	doorSyncConsumer.Start()
	if base.Websocket != nil {
		_ = base.Websocket.Connect()
	}
	monitor := systemmonitor.NewSystemMonitor(5)
	monitor.StartBackground()

	<-ctx.Done()
	registry.Stop()
	if base.Websocket != nil {
		base.Websocket.Close()
	}
	return ctx.Err()
}

func newServiceHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func rabbitBuilder(base *config.BaseConfig) *commonrbmq.BaseRabbitMQBuilder {
	return commonrbmq.NewRabbitMQBuilder().
		WithHost(base.RbmqHost).
		WithPort(base.RbmqPort).
		WithCredential(base.RbmqUsername, base.RbmqPassword).
		WithVHost(base.RbmqVirtualHost)
}
