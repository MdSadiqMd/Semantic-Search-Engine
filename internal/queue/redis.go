package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

type Job struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Data       map[string]interface{} `json:"data"`
	CreatedAt  time.Time              `json:"created_at"`
	Attempts   int                    `json:"attempts"`
	MaxRetries int                    `json:"max_retries"`
}

func (q *RedisQueue) Push(ctx context.Context, job *Job) error {
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

func (q *RedisQueue) Pop(ctx context.Context, timeout time.Duration) (*Job, error) {
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

	return &job, nil
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

func (ps *RedisPubSub) Subscribe(ctx context.Context, channel string) <-chan *redis.Message {
	pubsub := ps.client.Subscribe(ctx, channel)
	return pubsub.Channel()
}

func (ps *RedisPubSub) Close() error {
	return ps.client.Close()
}
