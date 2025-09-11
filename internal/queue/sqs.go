package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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

func (q *SQSQueue) Dequeue(ctx context.Context) ([]QueueMessage, error) {
	result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to receive messages from SQS: %w", err)
	}

	var messages []QueueMessage
	for _, sqsMsg := range result.Messages {
		var msg QueueMessage
		if err := json.Unmarshal([]byte(*sqsMsg.Body), &msg); err != nil {
			q.logger.Error("Failed to unmarshal SQS message", zap.Error(err))
			continue
		}

		msg.ID = *sqsMsg.ReceiptHandle
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

// SQS client doesn't need explicit closing, lol
func (q *SQSQueue) Close() error {
	return nil
}

type QueueMessage struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
