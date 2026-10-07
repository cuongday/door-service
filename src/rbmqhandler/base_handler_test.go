package rbmqhandler_test

import (
	"encoding/json"
	"testing"

	"doorservice/manager"
	"doorservice/rbmqhandler"

	"github.com/stretchr/testify/require"
)

func TestRBMQMessageKeepsCompatibilityFields(t *testing.T) {
	raw := []byte(`{"id":"req-1","event":"OPEN_DOOR","data":{"doorId":"door-1"},"dst":"DOOR_svc_command","replyQueueName":"AI_REPLY"}`)
	var msg rbmqhandler.RBMQMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	require.Equal(t, "req-1", msg.ID)
	require.JSONEq(t, `{"doorId":"door-1"}`, string(msg.Data))
}

func TestSanitizeForLogNeverReturnsSecrets(t *testing.T) {
	sanitized := rbmqhandler.SanitizeForLog([]byte(`{"password":"secret","nested":{"passwords":["one","two"],"authorization":"Digest abc"}}`))
	require.NotContains(t, sanitized, "secret")
	require.NotContains(t, sanitized, "one")
	require.NotContains(t, sanitized, "Digest abc")
	require.Contains(t, sanitized, rbmqhandler.RedactedValue)
}

func TestPublishErrorPreservesRequestAndReplyQueue(t *testing.T) {
	publisher := &capturePublisher{}
	handler := rbmqhandler.NewBaseHandler(publisher)
	request := rbmqhandler.RBMQMessage{
		ID:             "req-1",
		Event:          rbmqhandler.EventOpenDoor,
		ReplyQueueName: "AI_REPLY",
	}

	require.NoError(t, handler.PublishError(request, manager.ErrorDoorNotFound, "door not found"))
	require.Equal(t, "AI_REPLY", publisher.queue)
	require.Equal(t, "req-1", publisher.message.ID)
	require.Equal(t, rbmqhandler.EventError, publisher.message.Event)
	require.JSONEq(t, `{"code":"DOOR_NOT_FOUND","message":"door not found"}`, string(publisher.message.Data))
}

type capturePublisher struct {
	queue   string
	message rbmqhandler.RBMQMessage
}

func (p *capturePublisher) Publish(queue string, message rbmqhandler.RBMQMessage) error {
	p.queue = queue
	p.message = message
	return nil
}
