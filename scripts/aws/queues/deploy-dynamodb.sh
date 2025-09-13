#!/bin/bash
set -euo pipefail

TABLE_PREFIX="${PROJECT_NAME:-semantic-search-engine}"
REGION="${AWS_REGION:-ap-south-1}"

echo "Creating DynamoDB tables in region: $REGION"

wait_for_active() {
  local table=$1
  echo "Waiting for $table to be ACTIVE..."
  while true; do
    status=$(aws dynamodb describe-table \
      --table-name "$table" \
      --region "$REGION" \
      --query "Table.TableStatus" \
      --output text 2>/dev/null || echo "MISSING")
    if [ "$status" == "ACTIVE" ]; then
      echo "$table is ACTIVE"
      break
    elif [ "$status" == "MISSING" ]; then
      echo "❌ $table not found, aborting"
      exit 1
    else
      echo "$table is still $status..."
      sleep 5
    fi
  done
}

enable_ttl() {
  local table=$1
  local attr=$2
  echo "Enabling TTL on $table ($attr)..."
  aws dynamodb update-time-to-live \
    --table-name "$table" \
    --region "$REGION" \
    --time-to-live-specification "Enabled=true, AttributeName=$attr" >/dev/null
}

create_table() {
  local name=$1
  local spec=$2
  echo "Creating $name table..."
  if aws dynamodb create-table \
    --cli-input-json "$spec" \
    --region "$REGION" > /dev/null 2> err.log; then
    echo "$name table creation started"
  else
    if grep -q "Table already exists" err.log; then
      echo "$name table already exists"
    else
      echo "❌ Failed to create $name table"
      cat err.log
      exit 1
    fi
  fi
}

connections_spec=$(cat <<EOF
{
  "TableName": "${TABLE_PREFIX}-connections",
  "AttributeDefinitions": [
    {"AttributeName": "connection_id", "AttributeType": "S"}
  ],
  "KeySchema": [
    {"AttributeName": "connection_id", "KeyType": "HASH"}
  ],
  "BillingMode": "PAY_PER_REQUEST"
}
EOF
)

events_spec=$(cat <<EOF
{
  "TableName": "${TABLE_PREFIX}-events",
  "AttributeDefinitions": [
    {"AttributeName": "event_id", "AttributeType": "S"},
    {"AttributeName": "topic", "AttributeType": "S"},
    {"AttributeName": "timestamp", "AttributeType": "N"}
  ],
  "KeySchema": [
    {"AttributeName": "event_id", "KeyType": "HASH"}
  ],
  "BillingMode": "PAY_PER_REQUEST",
  "GlobalSecondaryIndexes": [
    {
      "IndexName": "topic-timestamp-index",
      "KeySchema": [
        {"AttributeName": "topic", "KeyType": "HASH"},
        {"AttributeName": "timestamp", "KeyType": "RANGE"}
      ],
      "Projection": {"ProjectionType": "ALL"}
    }
  ]
}
EOF
)

projects_spec=$(cat <<EOF
{
  "TableName": "${TABLE_PREFIX}-projects",
  "AttributeDefinitions": [
    {"AttributeName": "id", "AttributeType": "S"}
  ],
  "KeySchema": [
    {"AttributeName": "id", "KeyType": "HASH"}
  ],
  "BillingMode": "PAY_PER_REQUEST"
}
EOF
)

elements_spec=$(cat <<EOF
{
  "TableName": "${TABLE_PREFIX}-elements",
  "AttributeDefinitions": [
    {"AttributeName": "id", "AttributeType": "S"},
    {"AttributeName": "projectId", "AttributeType": "S"}
  ],
  "KeySchema": [
    {"AttributeName": "id", "KeyType": "HASH"}
  ],
  "BillingMode": "PAY_PER_REQUEST",
  "GlobalSecondaryIndexes": [
    {
      "IndexName": "projectId-index",
      "KeySchema": [
        {"AttributeName": "projectId", "KeyType": "HASH"}
      ],
      "Projection": {"ProjectionType": "ALL"}
    }
  ]
}
EOF
)

jobs_spec=$(cat <<EOF
{
  "TableName": "${TABLE_PREFIX}-jobs",
  "AttributeDefinitions": [
    {"AttributeName": "id", "AttributeType": "S"},
    {"AttributeName": "projectId", "AttributeType": "S"}
  ],
  "KeySchema": [
    {"AttributeName": "id", "KeyType": "HASH"}
  ],
  "BillingMode": "PAY_PER_REQUEST",
  "GlobalSecondaryIndexes": [
    {
      "IndexName": "projectId-index",
      "KeySchema": [
        {"AttributeName": "projectId", "KeyType": "HASH"}
      ],
      "Projection": {"ProjectionType": "ALL"}
    }
  ]
}
EOF
)

create_table "connections" "$connections_spec"
create_table "events" "$events_spec"
create_table "projects" "$projects_spec"
create_table "elements" "$elements_spec"
create_table "jobs" "$jobs_spec"

wait_for_active "${TABLE_PREFIX}-connections"
wait_for_active "${TABLE_PREFIX}-events"
wait_for_active "${TABLE_PREFIX}-projects"
wait_for_active "${TABLE_PREFIX}-elements"
wait_for_active "${TABLE_PREFIX}-jobs"

enable_ttl "${TABLE_PREFIX}-connections" "ttl"
enable_ttl "${TABLE_PREFIX}-events" "ttl"

cat > .aws-dynamodb-tables <<EOF
CONNECTIONS_TABLE=${TABLE_PREFIX}-connections
EVENTS_TABLE=${TABLE_PREFIX}-events
PROJECTS_TABLE=${TABLE_PREFIX}-projects
ELEMENTS_TABLE=${TABLE_PREFIX}-elements
JOBS_TABLE=${TABLE_PREFIX}-jobs
EOF

echo "✅ DynamoDB deployment completed:"
echo "  Connections: ${TABLE_PREFIX}-connections (WebSocket connections)"
echo "  Events: ${TABLE_PREFIX}-events (Real-time events)"
echo "  Projects: ${TABLE_PREFIX}-projects (Code projects)"
echo "  Elements: ${TABLE_PREFIX}-elements (Code elements/functions)"
echo "  Jobs: ${TABLE_PREFIX}-jobs (Analysis jobs)"