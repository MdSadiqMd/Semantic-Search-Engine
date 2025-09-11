package types

import (
	"context"
	"time"
)

type QueueMessage struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type Queue interface {
	Enqueue(ctx context.Context, jobType string, payload interface{}) error
	Dequeue(ctx context.Context, timeout time.Duration) ([]QueueMessage, error)
	Delete(ctx context.Context, messageID string) error
	Size(ctx context.Context) (int64, error)
	Clear(ctx context.Context) error
	Close() error
}

type PubSub interface {
    Publish(ctx context.Context, topic string, message interface{}) error
    Subscribe(ctx context.Context, topic string) (<-chan interface{}, error)
    Close() error
}