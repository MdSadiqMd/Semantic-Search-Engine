#!/bin/bash
set -euo pipefail

API_NAME="${PROJECT_NAME:-semantic-search-service}-api"
REGION="${AWS_REGION:-ap-south-1}"

if [ -f ".aws-lambda-function" ]; then
    source .aws-lambda-function
else
    echo "❌ Missing .aws-lambda-function file. Run deploy-lambda.sh first."
    exit 1
fi

echo "Looking for existing API Gateway: $API_NAME"

API_ID=$(aws apigatewayv2 get-apis \
    --region "$REGION" \
    --query "Items[?Name=='$API_NAME'].ApiId" \
    --output text)

if [ -z "$API_ID" ] || [ "$API_ID" == "None" ]; then
    echo "Creating new API Gateway..."
        API_ID=$(aws apigatewayv2 create-api \
        --name "$API_NAME" \
        --protocol-type HTTP \
        --cors-configuration '{
            "AllowHeaders": ["content-type", "x-amz-date", "authorization", "x-api-key", "x-amz-security-token", "cache-control", "accept"],
            "AllowMethods": ["GET", "POST", "PUT", "DELETE", "OPTIONS"],
            "AllowOrigins": ["*"],
            "ExposeHeaders": ["content-type", "cache-control"],
            "MaxAge": 300
        }' \
        --region "$REGION" \
        --query 'ApiId' \
        --output text)
    echo "Created API with ID: $API_ID"
else
    echo "Found existing API with ID: $API_ID"
fi

echo "Creating integration with Lambda: $FUNCTION_ARN"
INTEGRATION_ID=$(aws apigatewayv2 create-integration \
    --api-id "$API_ID" \
    --integration-type AWS_PROXY \
    --integration-uri "$FUNCTION_ARN" \
    --payload-format-version "2.0" \
    --region "$REGION" \
    --query 'IntegrationId' \
    --output text)

echo "Integration created with ID: $INTEGRATION_ID"

declare -a routes=(
    "GET /api/projects"
    "POST /api/projects"
    "GET /api/projects/{projectId}"
    "PUT /api/projects/{projectId}"
    "DELETE /api/projects/{projectId}"
    "GET /api/projects/{projectId}/elements"
    "POST /api/projects/{projectId}/elements"
    "GET /api/elements/{elementId}"
    "PUT /api/elements/{elementId}"
    "DELETE /api/elements/{elementId}"
    "POST /api/search"
    "GET /api/projects/{projectId}/graph"
    "GET /api/elements/{elementId}/connections"
    "POST /api/projects/{projectId}/analyze"
    "GET /api/jobs/{jobId}"
    "GET /api/projects/{projectId}/jobs"
    "GET /api/projects/{projectId}/stats"
    "GET /api/health"
)

echo "Creating routes..."
for route in "${routes[@]}"; do
    echo " → $route"
    aws apigatewayv2 create-route \
        --api-id "$API_ID" \
        --route-key "$route" \
        --target "integrations/$INTEGRATION_ID" \
        --region "$REGION" >/dev/null 2>&1 || echo "   (already exists)"
done

STAGE_NAME='$default'
echo "Deploying stage: $STAGE_NAME"
aws apigatewayv2 create-stage \
  --api-id "$API_ID" \
  --stage-name "$STAGE_NAME" \
  --auto-deploy \
  --region "$REGION" >/dev/null 2>&1 || echo "   (already exists)"

echo "Adding Lambda invoke permission"
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
aws lambda add-permission \
    --function-name "$FUNCTION_NAME" \
    --statement-id "apigateway-invoke" \
    --action "lambda:InvokeFunction" \
    --principal "apigateway.amazonaws.com" \
    --source-arn "arn:aws:execute-api:$REGION:$ACCOUNT_ID:$API_ID/*/*" \
    --region "$REGION" >/dev/null 2>&1 || echo "   (already exists)"

API_ENDPOINT=$(aws apigatewayv2 get-api \
    --api-id "$API_ID" \
    --region "$REGION" \
    --query 'ApiEndpoint' \
    --output text)

cat > .aws-apigateway <<EOF
API_ID=$API_ID
API_ENDPOINT=$API_ENDPOINT
INTEGRATION_ID=$INTEGRATION_ID
STAGE_NAME='$STAGE_NAME'
EOF

echo ""
echo "✅ API Gateway deployment completed:"
echo "  API ID: $API_ID"
echo "  Endpoint: $API_ENDPOINT"
echo "  Stage: $STAGE_NAME"
echo "🧪 Test URLs:"
echo "  Health Check: $API_ENDPOINT/api/health"
echo "  Projects: $API_ENDPOINT/api/projects"
echo "  Search: $API_ENDPOINT/api/search"