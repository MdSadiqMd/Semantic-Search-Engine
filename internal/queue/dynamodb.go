package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"go.uber.org/zap"
)

type DynamoDBAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

type DynamoPubSub struct {
	client    DynamoDBAPI
	tableName string
	logger    *zap.Logger
}

type ConnectionRecord struct {
	ConnectionID string    `json:"connection_id" dynamodbav:"connection_id"`
	ProjectID    string    `json:"project_id,omitempty" dynamodbav:"project_id,omitempty"`
	UserID       string    `json:"user_id,omitempty" dynamodbav:"user_id,omitempty"`
	ConnectedAt  time.Time `json:"connected_at" dynamodbav:"connected_at"`
	TTL          int64     `json:"ttl" dynamodbav:"ttl"`
}

type EventMessage struct {
	EventType string      `json:"event_type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

func NewDynamoPubSubWithClient(client DynamoDBAPI, tableName string, logger *zap.Logger) *DynamoPubSub {
	return &DynamoPubSub{
		client:    client,
		tableName: tableName,
		logger:    logger,
	}
}

func NewDynamoPubSub(tableName string, logger *zap.Logger) (*DynamoPubSub, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	client := dynamodb.NewFromConfig(cfg)
	return NewDynamoPubSubWithClient(client, tableName, logger), nil
}

func (d *DynamoPubSub) StoreConnection(ctx context.Context, connectionID, projectID, userID string) error {
	record := ConnectionRecord{
		ConnectionID: connectionID,
		ProjectID:    projectID,
		UserID:       userID,
		ConnectedAt:  time.Now(),
		TTL:          time.Now().Add(24 * time.Hour).Unix(),
	}

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return fmt.Errorf("marshal connection record: %w", err)
	}

	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("store connection: %w", err)
	}

	d.logger.Info("Connection stored", zap.String("connection_id", connectionID))
	return nil
}

func (d *DynamoPubSub) RemoveConnection(ctx context.Context, connectionID string) error {
	key, err := attributevalue.MarshalMap(map[string]string{"connection_id": connectionID})
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}

	_, err = d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key:       key,
	})
	if err != nil {
		return fmt.Errorf("remove connection: %w", err)
	}

	d.logger.Info("Connection removed", zap.String("connection_id", connectionID))
	return nil
}

func (d *DynamoPubSub) GetConnectionsByProject(ctx context.Context, projectID string) ([]string, error) {
	out, err := d.client.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(d.tableName),
		FilterExpression: aws.String("project_id = :pid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pid": &types.AttributeValueMemberS{Value: projectID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("scan connections: %w", err)
	}

	var ids []string
	for _, item := range out.Items {
		var rec ConnectionRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			d.logger.Error("unmarshal connection record", zap.Error(err))
			continue
		}
		ids = append(ids, rec.ConnectionID)
	}
	return ids, nil
}

func (d *DynamoPubSub) Publish(ctx context.Context, topic string, message interface{}) error {
	event := EventMessage{
		EventType: topic,
		Data:      message,
		Timestamp: time.Now(),
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	item := map[string]types.AttributeValue{
		"event_id":  &types.AttributeValueMemberS{Value: fmt.Sprintf("%s_%d", topic, time.Now().UnixNano())},
		"topic":     &types.AttributeValueMemberS{Value: topic},
		"data":      &types.AttributeValueMemberS{Value: string(eventJSON)},
		"timestamp": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Unix())},
		"ttl":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Add(1*time.Hour).Unix())},
	}

	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName + "_events"),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	d.logger.Info("Event published", zap.String("topic", topic))
	return nil
}

func (d *DynamoPubSub) Subscribe(ctx context.Context, topic string) (<-chan interface{}, error) {
	ch := make(chan interface{})
	go func() {
		defer close(ch)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				since := time.Now().Add(-5 * time.Second).Unix()
				out, err := d.client.Query(ctx, &dynamodb.QueryInput{
					TableName:              aws.String(d.tableName + "_events"),
					IndexName:              aws.String("topic-timestamp-index"),
					KeyConditionExpression: aws.String("topic = :t AND #ts > :since"),
					ExpressionAttributeNames: map[string]string{
						"#ts": "timestamp",
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":t":     &types.AttributeValueMemberS{Value: topic},
						":since": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", since)},
					},
				})
				if err != nil {
					d.logger.Error("query events", zap.Error(err))
					continue
				}
				for _, item := range out.Items {
					raw, ok := item["data"].(*types.AttributeValueMemberS)
					if !ok {
						continue
					}
					var evt EventMessage
					if err := json.Unmarshal([]byte(raw.Value), &evt); err != nil {
						d.logger.Error("unmarshal event", zap.Error(err))
						continue
					}
					select {
					case ch <- evt.Data:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

func (d *DynamoPubSub) Close() error {
	return nil
}

// ignore, for tests, i give up, this is the only way
func (d *DynamoPubSub) TestClient() interface{} {
	return d.client
}
