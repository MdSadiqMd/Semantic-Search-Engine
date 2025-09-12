#!/bin/bash
set -e

QUEUE_NAME="${PROJECT_NAME:-semantic-search-service}-analysis-jobs"
REGION="${AWS_REGION:-ap-south-1}"

echo "Creating SQS queue: $QUEUE_NAME"

QUEUE_URL=$(aws sqs create-queue \
    --queue-name "$QUEUE_NAME" \
    --region "$REGION" \
    --attributes "VisibilityTimeout=300,MessageRetentionPeriod=1209600,ReceiveMessageWaitTimeSeconds=20" \
    --query 'QueueUrl' \
    --output text 2>/dev/null || \
    aws sqs get-queue-url --queue-name "$QUEUE_NAME" --region "$REGION" --query 'QueueUrl' --output text)

DLQ_NAME="${QUEUE_NAME}-dlq"
DLQ_URL=$(aws sqs create-queue \
    --queue-name "$DLQ_NAME" \
    --region "$REGION" \
    --query 'QueueUrl' \
    --output text 2>/dev/null || \
    aws sqs get-queue-url --queue-name "$DLQ_NAME" --region "$REGION" --query 'QueueUrl' --output text)

QUEUE_ARN=$(aws sqs get-queue-attributes \
    --queue-url "$QUEUE_URL" \
    --attribute-names QueueArn \
    --region "$REGION" \
    --query 'Attributes.QueueArn' \
    --output text)

DLQ_ARN=$(aws sqs get-queue-attributes \
    --queue-url "$DLQ_URL" \
    --attribute-names QueueArn \
    --region "$REGION" \
    --query 'Attributes.QueueArn' \
    --output text)

# dead letter queue policy
aws sqs set-queue-attributes \
    --queue-url "$QUEUE_URL" \
    --attributes "{\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"$DLQ_ARN\\\",\\\"maxReceiveCount\\\":3}\"}" \
    --region "$REGION"

cat > .aws-sqs-queues <<EOF
QUEUE_URL=$QUEUE_URL
DLQ_URL=$DLQ_URL
QUEUE_ARN=$QUEUE_ARN
DLQ_ARN=$DLQ_ARN
EOF

echo "✅ SQS deployment completed:"
echo "  Main Queue: $QUEUE_URL"
echo "  Dead Letter Queue: $DLQ_URL"
