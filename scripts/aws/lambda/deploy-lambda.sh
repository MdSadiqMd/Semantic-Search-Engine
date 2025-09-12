#!/bin/bash
set -e

FUNCTION_NAME="${PROJECT_NAME:-semantic-search-service}-api"
REGION="${AWS_REGION:-ap-south-1}"
ROLE_NAME="${FUNCTION_NAME}-role"

source .aws-dynamodb-tables
source .aws-sqs-queues
BUCKET_NAME=$(cat .aws-bucket-name)
CONFIG_FILE="config/config.yaml"

if [ ! -f "function.zip" ]; then
    echo "❌ function.zip not found. Run ./scripts/aws/lambda/build-lambda.sh first."
    exit 1
fi

POSTGRES_DSN=$(yq -r '.database.postgres.dsn' "$CONFIG_FILE")
NEO4J_URI=$(yq -r '.database.neo4j.uri' "$CONFIG_FILE")
NEO4J_USERNAME=$(yq -r '.database.neo4j.username' "$CONFIG_FILE")
NEO4J_PASSWORD=$(yq -r '.database.neo4j.password' "$CONFIG_FILE")
REDIS_ADDR=$(yq -r '.database.redis.addr' "$CONFIG_FILE")
REDIS_PASSWORD=$(yq -r '.database.redis.password' "$CONFIG_FILE")
REDIS_DB=$(yq -r '.database.redis.db' "$CONFIG_FILE")
EMBEDDING_PROVIDER=$(yq -r '.embedding.provider' "$CONFIG_FILE")
EMBEDDING_MODEL=$(yq -r '.embedding.model' "$CONFIG_FILE")
EMBEDDING_ENDPOINT=$(yq -r '.embedding.endpoint' "$CONFIG_FILE")
EMBEDDING_API_KEY=$(yq -r '.embedding.api_key' "$CONFIG_FILE")

echo "Uploading Go Lambda function to S3..."
aws s3 cp function.zip "s3://$BUCKET_NAME/function.zip"

echo "Checking/creating Lambda execution role..."
if aws iam get-role --role-name "$ROLE_NAME" --region "$REGION" >/dev/null 2>&1; then
    echo "Role $ROLE_NAME already exists. Skipping creation."
    ROLE_ARN=$(aws iam get-role --role-name "$ROLE_NAME" --query 'Role.Arn' --output text --region "$REGION")
else
    ROLE_ARN=$(aws iam create-role \
        --role-name "$ROLE_NAME" \
        --assume-role-policy-document '{
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Principal": { "Service": "lambda.amazonaws.com" },
                    "Action": "sts:AssumeRole"
                }
            ]
        }' \
        --query 'Role.Arn' \
        --output text \
        --region "$REGION")

    aws iam attach-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-arn "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"

    CUSTOM_POLICY_DOCUMENT=$(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:Scan"
      ],
      "Resource": [
        "arn:aws:dynamodb:$REGION:*:table/$CONNECTIONS_TABLE",
        "arn:aws:dynamodb:$REGION:*:table/$EVENTS_TABLE",
        "arn:aws:dynamodb:$REGION:*:table/$PROJECTS_TABLE",
        "arn:aws:dynamodb:$REGION:*:table/$ELEMENTS_TABLE",
        "arn:aws:dynamodb:$REGION:*:table/$JOBS_TABLE",
        "arn:aws:dynamodb:$REGION:*:table/*/index/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "sqs:SendMessage",
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": [
        "$QUEUE_ARN",
        "$DLQ_ARN"
      ]
    }
  ]
}
EOF
)

    aws iam put-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-name "${ROLE_NAME}-policy" \
        --policy-document "$CUSTOM_POLICY_DOCUMENT"

    echo "Waiting for role propagation..."
    sleep 10
fi

ENV_VARS="Variables={\
CONNECTIONS_TABLE=\"$CONNECTIONS_TABLE\",\
EVENTS_TABLE=\"$EVENTS_TABLE\",\
PROJECTS_TABLE=\"$PROJECTS_TABLE\",\
ELEMENTS_TABLE=\"$ELEMENTS_TABLE\",\
JOBS_TABLE=\"$JOBS_TABLE\",\
SQS_QUEUE_URL=\"$QUEUE_URL\",\
POSTGRES_DSN=\"$POSTGRES_DSN\",\
NEO4J_URI=\"$NEO4J_URI\",\
NEO4J_USERNAME=\"$NEO4J_USERNAME\",\
NEO4J_PASSWORD=\"$NEO4J_PASSWORD\",\
REDIS_ADDR=\"$REDIS_ADDR\",\
REDIS_PASSWORD=\"$REDIS_PASSWORD\",\
REDIS_DB=\"$REDIS_DB\",\
EMBEDDING_PROVIDER=\"$EMBEDDING_PROVIDER\",\
EMBEDDING_MODEL=\"$EMBEDDING_MODEL\",\
EMBEDDING_ENDPOINT=\"$EMBEDDING_ENDPOINT\",\
EMBEDDING_API_KEY=\"$EMBEDDING_API_KEY\"\
}"

echo "Creating/updating Lambda function..."
if aws lambda get-function --function-name "$FUNCTION_NAME" --region "$REGION" >/dev/null 2>&1; then
    echo "Updating existing function..."
    aws lambda update-function-code \
        --function-name "$FUNCTION_NAME" \
        --s3-bucket "$BUCKET_NAME" \
        --s3-key "function.zip" \
        --region "$REGION"

    aws lambda update-function-configuration \
        --function-name "$FUNCTION_NAME" \
        --environment "$ENV_VARS" \
        --region "$REGION"
else
    echo "Creating new function..."
    aws lambda create-function \
        --function-name "$FUNCTION_NAME" \
        --runtime "provided.al2" \
        --role "$ROLE_ARN" \
        --handler "bootstrap" \
        --code "S3Bucket=$BUCKET_NAME,S3Key=function.zip" \
        --timeout 30 \
        --memory-size 512 \
        --region "$REGION" \
        --environment "$ENV_VARS"
fi

FUNCTION_ARN=$(aws lambda get-function \
    --function-name "$FUNCTION_NAME" \
    --region "$REGION" \
    --query 'Configuration.FunctionArn' \
    --output text)

FUNCTION_URL=$(aws lambda get-function-url-config \
    --function-name "$FUNCTION_NAME" \
    --region "$REGION" \
    --query 'FunctionUrl' \
    --output text 2>/dev/null || true)

if [ "$FUNCTION_URL" == "None" ] || [ -z "$FUNCTION_URL" ]; then
    echo "Creating Lambda Function URL..."
    FUNCTION_URL=$(aws lambda create-function-url-config \
        --function-name "$FUNCTION_NAME" \
        --region "$REGION" \
        --auth-type NONE \
        --query 'FunctionUrl' \
        --output text)
fi

cat > .aws-lambda-function <<EOF
FUNCTION_NAME=$FUNCTION_NAME
FUNCTION_ARN=$FUNCTION_ARN
ROLE_ARN=$ROLE_ARN
EOF

echo "✅ Lambda deployment completed:"
echo "  Function: $FUNCTION_NAME ($FUNCTION_ARN)"
echo "  URL: $FUNCTION_URL"