#!/bin/bash
set -e

echo "Building tunnelchat (Go edition)..."
mkdir -p bin
go build -o bin/tunnelchat ./cmd/tunnelchat
echo "Build successful! Binary located at bin/tunnelchat"

if [ "$1" == "test" ]; then
    echo "Running tests..."
    go test -v ./...
fi
