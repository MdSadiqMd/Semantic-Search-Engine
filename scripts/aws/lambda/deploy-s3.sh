#!/bin/bash
set -e

BUCKET_NAME="${PROJECT_NAME:-semantic-search-service}-$(date +%s)"
REGION="${AWS_REGION:-ap-south-1}"

echo "Creating S3 bucket: $BUCKET_NAME"

aws s3api create-bucket \
    --bucket "$BUCKET_NAME" \
    --region "$REGION" \
    --create-bucket-configuration LocationConstraint="$REGION" \
    2>/dev/null || echo "Bucket creation skipped (may already exist)"

aws s3api put-bucket-versioning \
    --bucket "$BUCKET_NAME" \
    --versioning-configuration Status=Enabled

echo "Uploading function.zip to S3..."
aws s3 cp function.zip "s3://$BUCKET_NAME/function.zip"

echo "$BUCKET_NAME" > .aws-bucket-name
echo "✅ S3 deployment completed:"
echo "  Bucket: $BUCKET_NAME"
echo "  Function: s3://$BUCKET_NAME/function.zip"