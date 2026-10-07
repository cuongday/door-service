package discoverymanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"doorservice/dto"
	serviceManager "doorservice/manager"
	"doorservice/store"
)

const defaultMaxTargets = 4096

type Option func(*Manager)

func WithMaxTargets(max int) Option {
	return func(m *Manager) {
		if max > 0 {
			m.maxTargets = max
		}
	}
}

type Manager struct {
	adapter     serviceManager.DoorAdapter
	controllers *store.ControllerStore
	doors       *store.DoorStore
	jobs        *store.JobStore
	workerCount int
	maxTargets  int

	waitMu sync.Mutex
	waits  map[string]chan struct{}
}

type scanTarget struct {
	ip          string
	aiBoxID     string
	ports       []int
	credentials []credential
}

type credential struct {
	username string
	password string
}

func New(
	adapter serviceManager.DoorAdapter,
	controllers *store.ControllerStore,
	doors *store.DoorStore,
	jobs *store.JobStore,
	workerCount int,
	options ...Option,
) *Manager {
	if workerCount < 1 {
		workerCount = 1
	}
	m := &Manager{
		adapter:     adapter,
		controllers: controllers,
		doors:       doors,
		jobs:        jobs,
		workerCount: workerCount,
		maxTargets:  defaultMaxTargets,
		waits:       make(map[string]chan struct{}),
	}
	for _, option := range options {
		option(m)
	}
	return m
}

func (m *Manager) Start(
	parent context.Context,
	requestID string,
	input dto.DiscoveryDoorInfoDTO,
	onResult func(dto.DiscoveryResult),
	onDone func(),
) error {
	if strings.TrimSpace(requestID) == "" {
		return serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "request id is required", nil)
	}
	targets, err := m.targets(input)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(parent)
	if err := m.jobs.Add(requestID, cancel); err != nil {
		cancel()
		return serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "discovery request is already running", err)
	}
	done := make(chan struct{})
	m.waitMu.Lock()
	m.waits[requestID] = done
	m.waitMu.Unlock()

	go m.run(ctx, requestID, targets, onResult, onDone, done)
	return nil
}

func (m *Manager) Stop(requestID string) bool {
	return m.jobs.Cancel(requestID)
}

func (m *Manager) Wait(requestID string) {
	m.waitMu.Lock()
	done := m.waits[requestID]
	m.waitMu.Unlock()
	if done != nil {
		<-done
	}
}

func (m *Manager) run(
	ctx context.Context,
	requestID string,
	targets []scanTarget,
	onResult func(dto.DiscoveryResult),
	onDone func(),
	done chan struct{},
) {
	defer func() {
		m.jobs.Remove(requestID)
		if onDone != nil {
			onDone()
		}
		close(done)
		m.waitMu.Lock()
		delete(m.waits, requestID)
		m.waitMu.Unlock()
	}()

	work := make(chan scanTarget, len(targets))
	results := make(chan dto.DiscoveryResult, len(targets))
	for _, target := range targets {
		work <- target
	}
	close(work)

	workers := m.workerCount
	if workers > len(targets) {
		workers = len(targets)
	}
	var workerWG sync.WaitGroup
	workerWG.Add(workers)
	for range workers {
		go func() {
			defer workerWG.Done()
			for target := range work {
				results <- m.scan(ctx, target)
			}
		}()
	}
	go func() {
		workerWG.Wait()
		close(results)
	}()

	for result := range results {
		if onResult != nil {
			onResult(result)
		}
	}
}

func (m *Manager) scan(ctx context.Context, target scanTarget) dto.DiscoveryResult {
	if err := ctx.Err(); err != nil {
		return cancelledResult(target)
	}

	var lastErr error
	var connected *dto.AccessControllerDTO
	for _, port := range target.ports {
		for _, auth := range target.credentials {
			if err := ctx.Err(); err != nil {
				return cancelledResult(target)
			}
			controller := dto.AccessControllerDTO{
				ID:        fmt.Sprintf("onvif://%s:%d", target.ip, port),
				Type:      "ONVIF",
				IPAddress: target.ip,
				Port:      port,
				Username:  auth.username,
				Password:  auth.password,
				Activate:  true,
				AIBoxID:   target.aiBoxID,
			}
			candidate, err := m.adapter.Connect(ctx, controller)
			if err != nil {
				lastErr = err
				continue
			}
			connected = &candidate
			doors, err := m.adapter.DiscoverDoors(ctx, candidate)
			if err != nil {
				if ctx.Err() != nil {
					return cancelledResult(target)
				}
				return m.partialResult(candidate, port, err)
			}
			_ = m.controllers.Upsert(candidate)
			for _, door := range doors {
				_ = m.doors.Upsert(door)
			}
			return dto.DiscoveryResult{
				AIBoxID:     target.aiBoxID,
				IP:          target.ip,
				Port:        port,
				ONVIFPort:   port,
				Username:    auth.username,
				Password:    auth.password,
				State:       true,
				ResultLevel: "success",
				Controller:  &candidate,
				Doors:       doors,
			}
		}
	}
	if ctx.Err() != nil {
		return cancelledResult(target)
	}
	if connected != nil {
		return m.partialResult(*connected, connected.Port, lastErr)
	}
	return failedResult(target, lastErr)
}

func (m *Manager) partialResult(controller dto.AccessControllerDTO, port int, err error) dto.DiscoveryResult {
	_ = m.controllers.Upsert(controller)
	result := dto.DiscoveryResult{
		AIBoxID:     controller.AIBoxID,
		IP:          controller.IPAddress,
		Port:        port,
		ONVIFPort:   port,
		Username:    controller.Username,
		Password:    controller.Password,
		State:       true,
		ResultLevel: "partial",
		Controller:  &controller,
	}
	applyError(&result, err)
	return result
}

func (m *Manager) targets(input dto.DiscoveryDoorInfoDTO) ([]scanTarget, error) {
	ips, err := expandIPv4RangeLimited(input.FromIP, input.ToIP, m.maxTargets)
	if err != nil {
		return nil, serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "invalid discovery IP range", err)
	}
	ports, err := discoveryPorts(input)
	if err != nil {
		return nil, err
	}
	credentials := credentialProduct(input.Usernames, input.Passwords)
	sort.Strings(ips)
	targets := make([]scanTarget, 0, len(ips))
	for _, ip := range ips {
		targets = append(targets, scanTarget{
			ip: ip, aiBoxID: input.AIBoxID, ports: ports, credentials: credentials,
		})
	}
	return targets, nil
}

func mergePorts(current, additions []int) []int {
	seen := make(map[int]struct{}, len(current)+len(additions))
	result := make([]int, 0, len(current)+len(additions))
	for _, ports := range [][]int{current, additions} {
		for _, port := range ports {
			if _, exists := seen[port]; exists {
				continue
			}
			seen[port] = struct{}{}
			result = append(result, port)
		}
	}
	return result
}

func mergeCredentials(current, additions []credential) []credential {
	seen := make(map[credential]struct{}, len(current)+len(additions))
	result := make([]credential, 0, len(current)+len(additions))
	for _, credentials := range [][]credential{current, additions} {
		for _, value := range credentials {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func discoveryPorts(input dto.DiscoveryDoorInfoDTO) ([]int, error) {
	ports := append([]int(nil), input.Ports...)
	if len(ports) == 0 && input.ONVIFPort != nil {
		ports = []int{*input.ONVIFPort}
	}
	if len(ports) == 0 {
		ports = []int{80}
	}
	seen := make(map[int]struct{}, len(ports))
	result := make([]int, 0, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return nil, serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "invalid discovery port", nil)
		}
		if _, exists := seen[port]; !exists {
			seen[port] = struct{}{}
			result = append(result, port)
		}
	}
	return result, nil
}

func credentialProduct(usernames, passwords []string) []credential {
	if len(usernames) == 0 {
		usernames = []string{""}
	}
	if len(passwords) == 0 {
		passwords = []string{""}
	}
	result := make([]credential, 0, len(usernames)*len(passwords))
	for _, username := range usernames {
		for _, password := range passwords {
			result = append(result, credential{username: username, password: password})
		}
	}
	return result
}

func cancelledResult(target scanTarget) dto.DiscoveryResult {
	return dto.DiscoveryResult{
		AIBoxID:      target.aiBoxID,
		IP:           target.ip,
		ResultLevel:  "cancelled",
		ErrorCode:    serviceManager.ErrorDiscoveryCancelled,
		ErrorMessage: "discovery was cancelled",
	}
}

func failedResult(target scanTarget, err error) dto.DiscoveryResult {
	result := dto.DiscoveryResult{AIBoxID: target.aiBoxID, IP: target.ip, ResultLevel: "unreachable"}
	var serviceErr *serviceManager.ServiceError
	if errors.As(err, &serviceErr) {
		switch serviceErr.Code {
		case serviceManager.ErrorAuthFailed:
			result.ResultLevel = "auth_failed"
		case serviceManager.ErrorUnsupportedProtocol:
			result.ResultLevel = "non_access_device"
		}
	}
	applyError(&result, err)
	return result
}

func applyError(result *dto.DiscoveryResult, err error) {
	if err == nil {
		return
	}
	var serviceErr *serviceManager.ServiceError
	if errors.As(err, &serviceErr) {
		result.ErrorCode = serviceErr.Code
		result.ErrorMessage = serviceErr.Message
		return
	}
	result.ErrorCode = serviceManager.ErrorDeviceUnreachable
	result.ErrorMessage = "device is unreachable"
}
