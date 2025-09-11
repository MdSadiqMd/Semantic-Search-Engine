package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	queuepkg "github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type mockDynamoClient struct {
	putItemInput    *dynamodb.PutItemInput
	deleteItemInput *dynamodb.DeleteItemInput
	scanInput       *dynamodb.ScanInput
	scanOutput      *dynamodb.ScanOutput
	queryInput      *dynamodb.QueryInput
	queryOutput     *dynamodb.QueryOutput
}

func (m *mockDynamoClient) PutItem(ctx context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.putItemInput = in
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoClient) DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if in == nil {
		return nil, errors.New("nil input")
	}
	m.deleteItemInput = in
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockDynamoClient) Scan(ctx context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	m.scanInput = in
	return m.scanOutput, nil
}

func (m *mockDynamoClient) Query(ctx context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.queryInput = in
	return m.queryOutput, nil
}

func TestStoreAndRemoveConnection(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockDynamoClient{}
	dsp := queuepkg.NewDynamoPubSubWithClient(mock, "tbl", logger)

	ctx := context.Background()
	err := dsp.StoreConnection(ctx, "cid", "pid", "uid")
	assert.NoError(t, err)
	assert.Equal(t, "tbl", *mock.putItemInput.TableName)
	assert.Contains(t, mock.putItemInput.Item, "connection_id")

	err = dsp.RemoveConnection(ctx, "cid")
	assert.NoError(t, err)
	assert.Equal(t, "tbl", *mock.deleteItemInput.TableName)
}

func TestGetConnectionsByProject(t *testing.T) {
	logger := zap.NewNop()
	dsp := queuepkg.NewDynamoPubSubWithClient(&mockDynamoClient{
		scanOutput: &dynamodb.ScanOutput{
			Items: []map[string]types.AttributeValue{
				{"connection_id": &types.AttributeValueMemberS{Value: "c1"}},
				{"connection_id": &types.AttributeValueMemberS{Value: "c2"}},
			},
		},
	}, "tbl", logger)

	ids, err := dsp.GetConnectionsByProject(context.Background(), "pid")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"c1", "c2"}, ids)
}

func TestPublishAndSubscribe(t *testing.T) {
	logger := zap.NewNop()
	dsp := queuepkg.NewDynamoPubSubWithClient(&mockDynamoClient{
		queryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				{"data": &types.AttributeValueMemberS{Value: `{"event_type":"t","data":42,"timestamp":"2025-09-11T00:00:00Z"}`}},
			},
		},
	}, "tbl", logger)

	err := dsp.Publish(context.Background(), "t", 42)
	assert.NoError(t, err)

	mockClient, ok := dsp.TestClient().(*mockDynamoClient)
	assert.True(t, ok)
	assert.Equal(t, "tbl_events", *mockClient.putItemInput.TableName)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	ch, err := dsp.Subscribe(ctx, "t")
	assert.NoError(t, err)

	val := <-ch
	assert.Equal(t, float64(42), val)
}
