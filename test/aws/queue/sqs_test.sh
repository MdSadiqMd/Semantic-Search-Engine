#!/usr/bin/env bash
set -euo pipefail

AWS_REGION=${AWS_REGION:-ap-south-1}
export AWS_REGION

QUEUE_NAME="test-queue-$(date +%s)"
echo "Creating SQS queue: $QUEUE_NAME in region $AWS_REGION"

QUEUE_URL=$(aws sqs create-queue \
  --region "$AWS_REGION" \
  --queue-name "$QUEUE_NAME" \
  --attributes VisibilityTimeout=30 \
  --query 'QueueUrl' --output text)
echo "Queue URL: $QUEUE_URL"

export SQS_QUEUE_URL="$QUEUE_URL"

echo "Running integration tests..."
go test ./test/aws/queue/sqs_test.go -tags=integration -v

echo "Deleting queue: $QUEUE_URL"
aws sqs delete-queue --region "$AWS_REGION" --queue-url "$QUEUE_URL"

echo "Integration test completed successfully."
