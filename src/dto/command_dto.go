package dto

import "time"

type DoorCommand struct {
	DoorID  string `json:"doorId"`
	Command string `json:"-"`
}

type CommandResult struct {
	RequestID    string    `json:"requestId"`
	DoorID       string    `json:"doorId"`
	Command      string    `json:"command"`
	Success      bool      `json:"success"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	CompletedAt  time.Time `json:"completedAt"`
}
