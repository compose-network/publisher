.PHONY: proto-gen proto-lint proto-format proto-breaking

# Generate protobuf files
proto-gen:
	@echo "Generating protobuf files..."
	buf generate

# Lint protobuf files
proto-lint:
	@echo "Linting protobuf files..."
	buf lint

# Format protobuf files
proto-format:
	@echo "Formatting protobuf files..."
	buf format -w

# Check for breaking changes
proto-breaking:
	@echo "Checking for breaking changes..."
	buf breaking --against '.git#branch=main'

# Update dependencies
proto-deps:
	@echo "Updating protobuf dependencies..."
	buf dep update
