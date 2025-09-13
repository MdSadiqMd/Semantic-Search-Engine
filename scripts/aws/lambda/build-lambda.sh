#!/bin/bash
set -e

echo "🔨 Building Go Lambda function..."

rm -f bootstrap function.zip

echo "Compiling Go binary..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bootstrap cmd/lambda/main.go
if [ ! -f "bootstrap" ]; then
    echo "❌ Failed to build Go binary"
    exit 1
fi

echo "Binary size: $(du -h bootstrap | cut -f1)"
echo "Creating function.zip..."
if [ -f "config/config.yaml" ]; then
    zip function.zip bootstrap config/config.yaml
else
    zip function.zip bootstrap
fi

rm bootstrap

echo "Package size: $(du -h function.zip | cut -f1)"
echo "✅ Lambda package built successfully!"