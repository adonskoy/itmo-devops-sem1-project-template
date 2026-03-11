#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
IMAGE_NAME="project-sem-1"

cd "$PROJECT_ROOT"

echo "Building Docker image: $IMAGE_NAME"
docker build -t "$IMAGE_NAME" .

echo "Docker image $IMAGE_NAME built successfully"
