.PHONY: server cli build-all

# Start the web studio server (serves UI + API on :28080)
server:
	cd generator && go build -o ../gen-server ./cmd/gen-server && cd .. && \
	./gen-server --root . --port 28080

# Build the CLI binary
cli:
	cd generator && go build -o ../gen ./cmd/gen && cd ..

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
