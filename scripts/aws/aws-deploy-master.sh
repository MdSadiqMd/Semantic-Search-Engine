#!/bin/bash
set -e

export PROJECT_NAME="${PROJECT_NAME:-semantic-search-service}"
export AWS_REGION="${AWS_REGION:-ap-south-1}"

echo "Checking Prerequisites"
if ! command -v aws &> /dev/null; then
    echo "❌ AWS CLI not found. Please install and configure AWS CLI."
    exit 1
fi
if ! command -v go &> /dev/null; then
    echo "❌ Go compiler not found. Please install Go 1.21 or later."
    exit 1
fi
if ! aws sts get-caller-identity &> /dev/null; then
    echo "❌ AWS CLI not configured. Please run 'aws configure'."
    exit 1
fi
echo "✅ Prerequisites check passed"

echo "Environment Variables Check"
required_vars=("NEON_DATABASE_URL" "NEO4J_URI" "NEO4J_USERNAME" "NEO4J_PASSWORD" "GOOGLE_AI_API_KEY")
missing_vars=()

for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
        missing_vars+=("$var")
    fi
done

if [ ${#missing_vars[@]} -ne 0 ]; then
    echo "❌ Missing required environment variables:"
    for var in "${missing_vars[@]}"; do
        echo "  - $var"
    done
    exit 1
fi
echo "✅ Environment variables verified"

echo "Starting AWS Deployment"
echo "Project: $PROJECT_NAME"
echo "Region: $AWS_REGION"
echo "Account: $(aws sts get-caller-identity --query Account --output text)"

echo "Step 1: Building Lambda Function"
./scripts/aws/lambda/build-lambda.sh
echo "Step 2: Deploying S3 Bucket"
./scripts/aws/lambda/deploy-s3.sh
echo "Step 3: Deploying DynamoDB Tables"
./scripts/aws/queues/deploy-dynamodb.sh
echo "Step 4: Deploying SQS Queue"
./scripts/aws/queues/deploy-sqs.sh
echo "Step 5: Deploying Lambda Function"
./scripts/aws/lambda/deploy-lambda.sh
echo "Step 6: Deploying API Gateway"
./scripts/aws/lambda/deploy-apigateway.sh

source .aws-apigateway
source .aws-lambda-function
source .aws-sqs-queues
source .aws-dynamodb-tables

echo -e "🎉 Deployment completed successfully!\n"

echo "Backend Services Deployed:"
echo "  ✓ Lambda Function: $FUNCTION_NAME"
echo "  ✓ API Gateway: $API_ID"
echo "  ✓ SQS Queue: $(basename $QUEUE_URL)"
echo "  ✓ DynamoDB Tables: 5 tables created"
echo "  ✓ S3 Bucket: $(cat .aws-bucket-name)"

echo "API Endpoints:"
echo "  Test the health endpoint: curl $API_ENDPOINT/api/health"
echo "  Monitor logs: aws logs tail /aws/lambda/$FUNCTION_NAME --follow"

cat > .env.deployed <<EOF
# Deployed AWS Backend Configuration
API_ENDPOINT=$API_ENDPOINT
AWS_REGION=$AWS_REGION
LAMBDA_FUNCTION=$FUNCTION_NAME
SQS_QUEUE_URL=$QUEUE_URL
DYNAMODB_CONNECTIONS_TABLE=$CONNECTIONS_TABLE
export REACT_APP_API_URL=$API_ENDPOINT
EOF

echo "✅ Environment configuration saved to .env.deployed"