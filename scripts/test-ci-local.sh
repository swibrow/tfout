#!/bin/bash
set -e

echo "🔧 Running local CI checks..."

echo "📦 Installing dependencies..."
make controller-gen envtest

echo "🧪 Running tests..."
make test

echo "🔍 Running linting..."
make lint

echo "🏗️  Building..."
make build

echo "🐳 Building Docker image..."
make docker-build

echo "📋 Generating installer..."
make build-installer

echo "✅ All CI checks passed!"
echo ""
echo "Next steps:"
echo "1. Commit your changes"
echo "2. Push to trigger GitHub Actions"
echo "3. Create a tag (e.g., v0.1.0) to trigger a release"