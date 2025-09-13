#!/bin/bash
set -euo pipefail

PROJECT="semantic-search-service"
REGION="ap-south-1"
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

echo "🧹 Starting cleanup for project: $PROJECT in region: $REGION"

LAMBDA_NAME="${PROJECT}-api"
echo "Deleting Lambda function: $LAMBDA_NAME"
if aws lambda get-function --function-name "$LAMBDA_NAME" --region "$REGION" >/dev/null 2>&1; then
  aws lambda delete-function --function-name "$LAMBDA_NAME" --region "$REGION"
  echo "   ✅Lambda function deleted"
else
  echo "   ❌Lambda function not found"
fi

if [[ -f ".aws-apigateway" ]]; then
  source .aws-apigateway
  echo "Deleting API Gateway: $API_ID"
  if aws apigatewayv2 get-api --api-id "$API_ID" --region "$REGION" >/dev/null 2>&1; then
    aws apigatewayv2 delete-api --api-id "$API_ID" --region "$REGION"
    echo "   ✅API Gateway deleted"
  else
    echo "   ❌API Gateway not found"
  fi
else
  echo "❌.aws-apigateway file not found, skipping API Gateway deletion"
fi

BUCKET_NAME=$(aws s3api list-buckets --query "Buckets[?starts_with(Name, \`${PROJECT}-\`)].Name" --output text | head -n1)
if [[ -n "$BUCKET_NAME" ]]; then
  echo "Emptying and deleting S3 bucket: $BUCKET_NAME"
  versions=$(aws s3api list-object-versions --bucket "$BUCKET_NAME" --query='{Objects: Versions[].{Key:Key,VersionId:VersionId}}' --output json || echo '{"Objects":[]}')
  markers=$(aws s3api list-object-versions --bucket "$BUCKET_NAME" --query='{Objects: DeleteMarkers[].{Key:Key,VersionId:VersionId}}' --output json || echo '{"Objects":[]}')
  
  if [[ "$versions" != '{"Objects":[]}' ]]; then
    aws s3api delete-objects --bucket "$BUCKET_NAME" --delete "$versions" || true
  fi
  if [[ "$markers" != '{"Objects":[]}' ]]; then
    aws s3api delete-objects --bucket "$BUCKET_NAME" --delete "$markers" || true
  fi

  aws s3api delete-bucket --bucket "$BUCKET_NAME" --region "$REGION"
  echo "   ✅S3 bucket deleted"
else
  echo "   ❌No S3 bucket found for $PROJECT"
fi

ROLE_NAME="${PROJECT}-api-role"
echo "Deleting IAM Role: $ROLE_NAME"
if aws iam get-role --role-name "$ROLE_NAME" >/dev/null 2>&1; then
  attached_policies=$(aws iam list-attached-role-policies --role-name "$ROLE_NAME" --query 'AttachedPolicies[].PolicyArn' --output text)
  for policy_arn in $attached_policies; do
    aws iam detach-role-policy --role-name "$ROLE_NAME" --policy-arn "$policy_arn"
  done

  inline_policies=$(aws iam list-role-policies --role-name "$ROLE_NAME" --query 'PolicyNames[]' --output text)
  for policy_name in $inline_policies; do
    aws iam delete-role-policy --role-name "$ROLE_NAME" --policy-name "$policy_name"
  done

  aws iam delete-role --role-name "$ROLE_NAME"
  echo "   ✅IAM role deleted"
else
  echo "   ❌IAM role not found"
fi

echo "Checking for AWS service-linked roles..."
for role in $(aws iam list-roles --query 'Roles[?starts_with(RoleName, `AWSServiceRoleForAPI`)].RoleName' --output text); do
  echo "Deleting service-linked role: $role"
  aws iam delete-service-linked-role --role-name "$role" || true
done

TABLES=(
  "${PROJECT}-connections"
  "${PROJECT}-events"
  "${PROJECT}-projects"
  "${PROJECT}-elements"
  "${PROJECT}-jobs"
)

for table in "${TABLES[@]}"; do
  echo "Deleting DynamoDB table: $table"
  if aws dynamodb describe-table --table-name "$table" --region "$REGION" >/dev/null 2>&1; then
    aws dynamodb delete-table --table-name "$table" --region "$REGION"
    echo "   ✅Table $table deletion started"
  else
    echo "   ❌Table $table not found"
  fi
done

QUEUES=(
  "${PROJECT}-analysis-jobs"
  "${PROJECT}-analysis-jobs-dlq"
)

for queue in "${QUEUES[@]}"; do
  QUEUE_URL=$(aws sqs get-queue-url --queue-name "$queue" --region "$REGION" --output text 2>/dev/null || true)
  if [[ -n "$QUEUE_URL" ]]; then
    echo "Deleting SQS queue: $QUEUE_URL"
    aws sqs delete-queue --queue-url "$QUEUE_URL" --region "$REGION"
    echo "   ✅Queue $queue deleted"
  else
    echo "   ❌Queue $queue not found"
  fi
done

rm -rf function.zip err.log .aws-apigateway .aws-lambda-function .aws-dynamodb-tables .aws-sqs-queues .aws-bucket-name .env.deployed
echo "✅ Cleanup completed successfully!"