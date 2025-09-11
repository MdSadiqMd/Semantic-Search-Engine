#!/usr/bin/env bash
set -euo pipefail

AWS_REGION=${AWS_REGION:-ap-south-1}
export AWS_REGION

TABLE="test-connections-$(date +%s)"
EVENT_TABLE="${TABLE}_events"

echo "Creating DynamoDB tables: $TABLE and $EVENT_TABLE"

aws dynamodb create-table \
  --region "$AWS_REGION" \
  --table-name "$TABLE" \
  --attribute-definitions AttributeName=connection_id,AttributeType=S \
  --key-schema AttributeName=connection_id,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST

aws dynamodb create-table \
  --region "$AWS_REGION" \
  --table-name "$EVENT_TABLE" \
  --attribute-definitions \
      AttributeName=event_id,AttributeType=S \
      AttributeName=topic,AttributeType=S \
      AttributeName=timestamp,AttributeType=N \
  --key-schema AttributeName=event_id,KeyType=HASH \
  --global-secondary-indexes \
    "IndexName=topic-timestamp-index,KeySchema=[{AttributeName=topic,KeyType=HASH},{AttributeName=timestamp,KeyType=RANGE}],Projection={ProjectionType=ALL}" \
  --billing-mode PAY_PER_REQUEST

wait_for_table_active() {
  local table_name=$1
  local region=$2
  echo "Waiting for table $table_name to become ACTIVE..."
  while true; do
    status=$(aws dynamodb describe-table --region "$region" --table-name "$table_name" --query "Table.TableStatus" --output text)
    echo "  Status: $status"
    if [ "$status" = "ACTIVE" ]; then
      break
    fi
    sleep 3
  done
}

wait_for_table_active "$TABLE" "$AWS_REGION"
wait_for_table_active "$EVENT_TABLE" "$AWS_REGION"

export DYNAMO_TABLE="$TABLE"

echo "Running integration tests..."
go test ./test/aws/queue/dynamodb_test.go -tags=integration -v

echo "Deleting tables..."
aws dynamodb delete-table --region "$AWS_REGION" --table-name "$TABLE"
aws dynamodb delete-table --region "$AWS_REGION" --table-name "$EVENT_TABLE"

echo "Integration tests complete."
