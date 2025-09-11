package queue_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	queuepkg "github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/types"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSQSQueue_Integration(t *testing.T) {
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		t.Skip("SQS_QUEUE_URL not set; skipping integration test")
	}

	_, err := config.LoadDefaultConfig(context.Background())
	assert.NoError(t, err)

	logger := zap.NewNop()
	q, err := queuepkg.NewSQSQueue(queueURL, logger)
	assert.NoError(t, err)

	ctx := context.Background()
	// Enqueue
	payload := map[string]string{"hello": "world"}
	err = q.Enqueue(ctx, "ping", payload)
	assert.NoError(t, err)

	// Dequeue
	var msgs []types.QueueMessage
	for range 5 {
		msgs, err = q.Dequeue(ctx, 5000)
		assert.NoError(t, err)
		if len(msgs) > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	assert.Greater(t, len(msgs), 0, "expected at least one message")

	// Validation
	msg := msgs[0]
	assert.Equal(t, "ping", msg.Type)
	var body map[string]string
	data, _ := json.Marshal(msg.Payload)
	_ = json.Unmarshal(data, &body)
	assert.Equal(t, payload, body)

	// Delete
	err = q.Delete(ctx, msg.ID)
	assert.NoError(t, err)
}
