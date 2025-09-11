package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/types"
	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client *redis.Client
	name   string
}

func NewRedisQueue(addr, password string, db int, queueName string) *RedisQueue {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &RedisQueue{
		client: client,
		name:   queueName,
	}
}

func (q *RedisQueue) Enqueue(ctx context.Context, jobType string, payload interface{}) error {
	job, ok := payload.(*Job)
	if !ok {
		return fmt.Errorf("invalid payload type, expected *Job")
	}

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	err = q.client.LPush(ctx, q.name, data).Err()
	if err != nil {
		return fmt.Errorf("failed to push job: %w", err)
	}

	return nil
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) ([]types.QueueMessage, error) {
	result, err := q.client.BRPop(ctx, timeout, q.name).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to pop job: %w", err)
	}

	if len(result) < 2 {
		return nil, fmt.Errorf("invalid result from redis")
	}

	var job Job
	err = json.Unmarshal([]byte(result[1]), &job)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	msg := types.QueueMessage{
		ID:      job.ID,
		Type:    job.Type,
		Payload: job.Data,
	}

	return []types.QueueMessage{msg}, nil
}

func (q *RedisQueue) Delete(ctx context.Context, messageID string) error {
	messages, err := q.client.LRange(ctx, q.name, 0, -1).Result()
	if err != nil {
		return err
	}

	for i, msg := range messages {
		var job Job
		if err := json.Unmarshal([]byte(msg), &job); err != nil {
			continue
		}
		if job.ID == messageID {
			return q.client.LSet(ctx, q.name, int64(i), "DELETED").Err()
		}
	}
	return nil
}

func (q *RedisQueue) Size(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.name).Result()
}

func (q *RedisQueue) Clear(ctx context.Context) error {
	return q.client.Del(ctx, q.name).Err()
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}

type RedisPubSub struct {
	client *redis.Client
}

func NewRedisPubSub(addr, password string, db int) *RedisPubSub {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &RedisPubSub{client: client}
}

func (ps *RedisPubSub) Publish(ctx context.Context, channel string, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return ps.client.Publish(ctx, channel, data).Err()
}

func (ps *RedisPubSub) Subscribe(ctx context.Context, channel string) (<-chan interface{}, error) {
	pubsub := ps.client.Subscribe(ctx, channel)
	ch := make(chan interface{})

	go func() {
		defer close(ch)
		for msg := range pubsub.Channel() {
			var data interface{}
			if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
				continue
			}
			ch <- data
		}
	}()

	return ch, nil
}

func (ps *RedisPubSub) Close() error {
	return ps.client.Close()
}
