package jobs

import (
	"encoding/json"

	"github.com/hibiken/asynq"
)

// Task represents a generic job task
type Task struct {
	Type    string
	Payload []byte
}

// NewTask creates a new task with JSON payload
func NewTask(taskType string, payload interface{}) (*asynq.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(taskType, data), nil
}

// ParsePayload parses task payload into the given struct
func ParsePayload(task *asynq.Task, dest interface{}) error {
	return json.Unmarshal(task.Payload(), dest)
}

// TaskHandler is a function that handles a task
type TaskHandler func(*asynq.Task) error
