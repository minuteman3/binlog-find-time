#!/bin/bash
set -e

# Run Go linters
echo "Running go vet..."
go vet ./...

echo "Running golint..."
command -v golint >/dev/null 2>&1 || { echo "Installing golint..."; go install golang.org/x/lint/golint@latest; }
golint -set_exit_status ./...

echo "Running errcheck..."
command -v errcheck >/dev/null 2>&1 || { echo "Installing errcheck..."; go install github.com/kisielk/errcheck@latest; }
errcheck ./...

# Run golangci-lint if available
if command -v golangci-lint >/dev/null 2>&1; then
  echo "Running golangci-lint..."
  golangci-lint run
else
  echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
fi

echo "All linting checks passed!"