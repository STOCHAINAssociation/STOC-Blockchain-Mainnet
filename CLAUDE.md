# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
STOC is a Cosmos SDK-based blockchain built with Ignite CLI. It's a sovereign blockchain with custom modules for securities tokenization and comprehensive tooling for development, testing, and deployment.

## Common Development Commands

### Build & Test
- `make install` - Build and install the stocd binary
- `make test` - Run full test suite (unit tests, vet, vulnerability checks)
- `make test-unit` - Run unit tests only  
- `make test-race` - Run tests with race condition detection
- `make test-cover` - Generate test coverage reports
- `make lint` - Run golangci-lint (15min timeout)
- `make lint-fix` - Auto-fix linting issues

### Development Workflow
- `ignite chain serve` - Start development blockchain with hot reload
- `ignite generate proto-go --yes` - Generate Go code from protobuf definitions
- `make proto-gen` - Generate protobuf code using Ignite CLI

### Running a Single Test
```bash
go test -mod=readonly -v -timeout 30m ./path/to/package -run TestFunctionName
```

## Architecture Overview

### Core Technology Stack
- **Cosmos SDK v0.53.0** - Main blockchain framework
- **Tendermint v0.38.17** - Consensus mechanism  
- **Ignite CLI** - Primary development and scaffolding tool
- **IBC v8.5.1** - Inter-Blockchain Communication protocol
- **Go 1.24.3** - Programming language

### Key Directories
- `app/` - Core blockchain application configuration
- `cmd/stocd/` - Binary CLI entry point
- `x/stoc/` - Custom STOC module (keeper, types, ante handlers)
- `proto/` - Protocol buffer definitions
- `api/` - Generated API code from protobuf
- `testutil/` - Test utilities and helpers

### Configuration Files
- `config.yml` - Ignite CLI configuration (accounts, validators, genesis)
- `go.mod` - Go dependencies with Cosmos SDK v0.53.0
- `proto/buf.yaml` - Protocol buffer configuration

## Development Environment
- **Chain ID**: `stoc`
- **Token**: `ustoc`
- **Local RPC**: http://localhost:26657
- **Local REST API**: http://localhost:1317
- **Local gRPC**: http://localhost:9090

## Network Information
- **Mainnet RPC**: https://rpc-stoc-mainnet.stochainscan.io/
- **Mainnet REST**: https://api-stoc-mainnet.stochainscan.io
- **Minimum Gas Price**: 0.001ustoc (mainnet), 0.0001ustoc (development)

## Code Generation Workflow
This project uses protobuf-first development. When modifying `.proto` files:
1. Update protobuf definitions in `proto/stoc/`
2. Run `make proto-gen` or `ignite generate proto-go --yes`
3. Generated code appears in `api/` and module directories
4. Run tests with `make test` before committing

## Module Structure
The custom `x/stoc` module follows standard Cosmos SDK patterns:
- `keeper/` - State management and business logic
- `types/` - Message types, errors, and codecs  
- `ante/` - Transaction ante handlers
- `module/` - Module interface implementation

## Testing Framework
- Uses standard Go testing with testify/require
- Simulation tests in `app/sim_test.go` for complex scenarios
- Module-specific tests in `x/stoc/` subdirectories
- Always run `make test` before commits to ensure quality

## Linting & Quality
- Uses golangci-lint v1.61.0 with 15-minute timeout
- Additional security scanning with govulncheck
- Auto-fix available with `make lint-fix`
- Mandatory before commits

## Important Work Guidelines

**Critical Rules:**
- **NEVER automatically run build and lint** after completing a request
- **Always use Vietnamese** in conversation and notes with users
- **Only use English** in code and code comments

## File Encoding Guidelines
When writing Vietnamese in files, ensure UTF-8 encoding is correct to avoid character errors. Use proper Vietnamese diacritics but pay attention to file format when using Write tool.