package rbmqhandler

import (
	"encoding/json"
	"strings"
	"time"
)

const RedactedValue = "[REDACTED]"

type RBMQMessage struct {
	ID             string          `json:"id"`
	CreatedAt      time.Time       `json:"createdAt"`
	Dst            string          `json:"dst"`
	Event          string          `json:"event"`
	Data           json.RawMessage `json:"data"`
	ReplyQueueName string          `json:"replyQueueName"`
	Username       string          `json:"username,omitempty"`
}

func SanitizeForLog(message []byte) string {
	var value any
	if err := json.Unmarshal(message, &value); err != nil {
		return "[UNPARSEABLE MESSAGE]"
	}
	redactValue(value)
	sanitized, err := json.Marshal(value)
	if err != nil {
		return "[UNPARSEABLE MESSAGE]"
	}
	return string(sanitized)
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSecretKey(key) {
				typed[key] = RedactedValue
				continue
			}
			redactValue(item)
		}
	case []any:
		for _, item := range typed {
			redactValue(item)
		}
	}
}

func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token")
}
