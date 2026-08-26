#!/bin/bash
set -e

IMAGE_NAME=${1:-task262-freeoverlap}
DOCKER_PLATFORM=${2:-linux/amd64}

docker build --platform "$DOCKER_PLATFORM" -f benzhi.Dockerfile -t "$IMAGE_NAME" .

echo ""
echo "✅ Docker image '$IMAGE_NAME' built successfully!"
echo ""
echo "📋 Next steps (for testing):"
echo "  • Smoke test：docker run --rm $IMAGE_NAME --smoke-test"
echo "  • Start server：docker run --rm -P $IMAGE_NAME --addr :8080 --db ./app.db"
echo ""
