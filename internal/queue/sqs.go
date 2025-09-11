package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.uber.org/zap"
)

type SQSQueue struct {
	client   *sqs.Client
	queueURL string
	logger   *zap.Logger
}

func NewSQSQueue(queueURL string, logger *zap.Logger) (*SQSQueue, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &SQSQueue{
		client:   sqs.NewFromConfig(cfg),
		queueURL: queueURL,
		logger:   logger,
	}, nil
}

func (q *SQSQueue) Enqueue(ctx context.Context, jobType string, payload interface{}) error {
	message := map[string]interface{}{
		"type":    jobType,
		"payload": payload,
	}

	messageBody, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(string(messageBody)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	q.logger.Info("Message sent to SQS", zap.String("type", jobType))
	return nil
}

func (q *SQSQueue) Dequeue(ctx context.Context, timeout time.Duration) ([]types.QueueMessage, error) {
	waitTime := min(int32(timeout.Seconds()), 20)

	result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     waitTime,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to receive messages from SQS: %w", err)
	}

	var messages []types.QueueMessage
	for _, sqsMsg := range result.Messages {
		var msgData map[string]interface{}
		if err := json.Unmarshal([]byte(*sqsMsg.Body), &msgData); err != nil {
			q.logger.Error("Failed to unmarshal SQS message", zap.Error(err))
			continue
		}

		msgType, ok := msgData["type"].(string)
		if !ok {
			q.logger.Error("Missing type in SQS message")
			continue
		}

		msg := types.QueueMessage{
			ID:      *sqsMsg.ReceiptHandle,
			Type:    msgType,
			Payload: msgData["payload"],
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (q *SQSQueue) Delete(ctx context.Context, messageID string) error {
	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(messageID),
	})
	if err != nil {
		return fmt.Errorf("failed to delete message from SQS: %w", err)
	}

	return nil
}

func (q *SQSQueue) Size(ctx context.Context) (int64, error) {
	result, err := q.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(q.queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			sqstypes.QueueAttributeNameApproximateNumberOfMessages,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get queue size: %w", err)
	}

	if count, ok := result.Attributes["ApproximateNumberOfMessages"]; ok {
		return strconv.ParseInt(count, 10, 64)
	}

	return 0, nil
}

func (q *SQSQueue) Clear(ctx context.Context) error {
	_, err := q.client.PurgeQueue(ctx, &sqs.PurgeQueueInput{
		QueueUrl: aws.String(q.queueURL),
	})
	if err != nil {
		return fmt.Errorf("failed to purge SQS queue: %w", err)
	}
	return nil
}

func (q *SQSQueue) Close() error {
	return nil
}
