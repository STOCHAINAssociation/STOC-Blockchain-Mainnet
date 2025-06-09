# STOC Blockchain Development Guide

This document provides comprehensive guidance for developers working on the STOC blockchain project.

## Development Environment Setup

### Prerequisites

- Go 1.24.3
- Node.js 18+ (for frontend development)
- Docker & Docker Compose
- Git
- Ignite CLI
- Protocol Buffers compiler (protoc)
- Minimum 16GB RAM
- 500GB+ available disk space
- Stable internet connection

### Installation

```bash
# Install Ignite CLI
curl https://get.ignite.com/cli! | bash

# Install protoc
# On macOS
brew install protobuf

# On Ubuntu/Debian
sudo apt install protobuf-compiler

# Install Go dependencies
go mod download
```

## Project Structure

```
STOC-Blockchain-Mainnet/
├── app/                    # Application configuration
├── cmd/                    # Command line interfaces
├── x/                      # Custom modules
│   ├── stoc/              # Main STOC module
│   └── ...                # Other custom modules
├── proto/                  # Protocol buffer definitions
├── docs/                   # Documentation
├── testutil/              # Test utilities
├── api/                   # Generated API files
└── tools/                 # Development tools
```

## Development Workflow

### 1. Local Development Setup

```bash
# Clone the repository
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

# Start development environment
ignite chain serve

# This will:
# - Build the chain
# - Initialize with test data
# - Start the chain locally
# - Enable hot reload for development
```

### 2. Creating Custom Modules

```bash
# Generate a new module
ignite scaffold module <module-name>

# Generate message types
ignite scaffold message <message-name> <field1> <field2> --module <module-name>

# Generate queries
ignite scaffold query <query-name> <field1> <field2> --module <module-name>

# Generate transactions
ignite scaffold transaction <tx-name> <field1> <field2> --module <module-name>
```

### 3. Protocol Buffer Development

```bash
# Generate protobuf files
make proto-gen

# Format protobuf files
make proto-format

# Lint protobuf files
make proto-lint
```

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific module tests
go test ./x/stoc/...
```

### Integration Tests

```bash
# Run integration tests
make test-integration

# Run end-to-end tests
make test-e2e
```

### Simulation Tests

```bash
# Run simulation tests
make test-sim

# Run simulation with specific parameters
go test -mod=readonly ./app -run TestFullAppSimulation -Enabled=true -NumBlocks=100 -BlockSize=200 -Commit=true -Seed=99 -Period=5 -v -timeout 24h
```

## Code Standards

### Go Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports`
- Write comprehensive tests
- Document public functions and types

```bash
# Format code
make format

# Lint code
make lint

# Security scan
make security
```

### Commit Guidelines

```
type(scope): description

Types:
- feat: New feature
- fix: Bug fix
- docs: Documentation changes
- style: Code style changes
- refactor: Code refactoring
- test: Test changes
- chore: Build/tooling changes

Example:
feat(stoc): add new token transfer functionality
```

## Module Development

### Creating a Custom Module

1. **Define the module structure**:
```go
// x/mymodule/types/genesis.go
type GenesisState struct {
    Params Params `protobuf:"bytes,1,opt,name=params,proto3" json:"params"`
}
```

2. **Implement keeper functions**:
```go
// x/mymodule/keeper/keeper.go
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
    store := ctx.KVStore(k.storeKey)
    bz := k.cdc.MustMarshal(&params)
    store.Set(types.ParamsKey, bz)
}
```

3. **Add message handlers**:
```go
// x/mymodule/keeper/msg_server.go
func (ms msgServer) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
    ctx := sdk.UnwrapSDKContext(goCtx)
    // Implementation
    return &types.MsgUpdateParamsResponse{}, nil
}
```

## API Development

### REST API

```bash
# Generate OpenAPI documentation
make proto-swagger-gen

# Start REST server
stocd start --api.enable=true --api.swagger=true
```

### gRPC API

```go
// Query client example
conn, err := grpc.Dial("localhost:9090", grpc.WithInsecure())
defer conn.Close()

client := types.NewQueryClient(conn)
res, err := client.Params(context.Background(), &types.QueryParamsRequest{})
```

## Frontend Development

### React/TypeScript Integration

```bash
# Install dependencies
npm install @cosmjs/stargate @cosmjs/proto-signing

# Generate TypeScript types
ignite generate ts-client
```

```typescript
// Example client usage
import { StocClient } from './client'

const client = new StocClient({
  apiURL: "http://localhost:1317",
  rpcURL: "http://localhost:26657",
})

// Query balance
const balance = await client.query.bank.balance(address, "ustoc")
```

## Debugging

### Local Debugging

```bash
# Enable debug mode
stocd start --log_level debug

# Use delve debugger
dlv debug ./cmd/stocd -- start
```

### Network Debugging

```bash
# Check node status
stocd status

# Query specific data
stocd query bank balances <address>

# Check transaction
stocd query tx <hash>
```

## Performance Optimization

### Profiling

```bash
# CPU profiling
go tool pprof http://localhost:6060/debug/pprof/profile

# Memory profiling
go tool pprof http://localhost:6060/debug/pprof/heap

# Enable profiling in config
echo 'profiling = true' >> ~/.stoc/config/config.toml
```

### Database Optimization

```bash
# Compact database
stocd compact

# Prune old data
stocd prune everything
```

## Security Best Practices

1. **Input Validation**: Always validate user inputs
2. **Access Control**: Implement proper permission checks
3. **Rate Limiting**: Prevent spam and DoS attacks
4. **Audit Logging**: Log important operations
5. **Secure Defaults**: Use secure configuration defaults

## Deployment

### Docker Development

```dockerfile
# Dockerfile.dev
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o stocd ./cmd/stocd

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/stocd /usr/local/bin/
EXPOSE 26656 26657 1317 9090
CMD ["stocd", "start"]
```

```bash
# Build and run
docker build -f Dockerfile.dev -t stoc-dev .
docker run -p 26657:26657 -p 1317:1317 stoc-dev
```

## Useful Development Commands

```bash
# Reset development chain
ignite chain serve --reset-once

# Build without starting
ignite chain build

# Update dependencies
go mod tidy

# Vendor dependencies
go mod vendor
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Run linting and tests
6. Submit a pull request

## Resources

- [Cosmos SDK Documentation](https://docs.cosmos.network/)
- [Ignite CLI Documentation](https://docs.ignite.com/)
- [Go Documentation](https://golang.org/doc/)
- [Protocol Buffers Guide](https://developers.google.com/protocol-buffers)

---

For questions and support, please refer to the GitHub issues or community channels. 