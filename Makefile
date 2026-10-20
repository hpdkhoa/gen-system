.PHONY: server cli build-all mcp review

# Start the web studio server (serves UI + API on :28080)
server:
	cd generator && go build -o ../gen-server ./cmd/gen-server && cd .. && \
	./gen-server --root . --port 28080

# Build the CLI binary
cli:
	cd generator && go build -o ../gen ./cmd/gen && cd ..

# Build the Understand MCP server (stdio; register it with your agent)
mcp:
	cd generator && go build -o ../understand-mcp ./cmd/understand-mcp && cd ..

# Build the change reviewer (Markdown or JSON; --fail-on break for CI)
review:
	cd generator && go build -o ../understand-review ./cmd/understand-review && cd ..

# Build both binaries
build-all: cli server

# Run tests
test:
	cd generator && go test ./...

# Quick dev loop: rebuild server and restart
dev:
	@# echo "Building gen-server..."
	cd generator && go build -o ../gen-server ./cmd/gen-server && cd ..
	@# echo "Starting on http://localhost:28080"
	./gen-server --root . --port 28080
