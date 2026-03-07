# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

STOC is a Cosmos SDK-based blockchain with EVM compatibility, built using Cosmos SDK v0.53.4 and Ignite CLI. It provides:

- **Custom Token System**: Securities tokenization module (`x/stoc`) for creating fungible tokens with tax support
- **EVM Integration**: Full Ethereum compatibility via `github.com/cosmos/evm` with dual denomination system
- **IBC Support**: Inter-blockchain communication for cross-chain operations

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

- **Cosmos SDK v0.53.4** - Main blockchain framework
- **CometBFT v0.38.18** - Consensus mechanism (formerly Tendermint)
- **Ignite CLI** - Primary development and scaffolding tool
- **IBC v10.2.0** - Inter-Blockchain Communication protocol
- **Cosmos EVM v1.0.0-rc2** - Ethereum Virtual Machine integration
- **Go 1.24.3** - Programming language

### Key Directories

- `app/` - Core blockchain application, EVM integration, ante handlers
  - `app/app.go` - Main application setup with dependency injection
  - `app/evm.go` - EVM module registration, precompiles, and custom activators
  - `app/ante/` - Transaction ante/post handler routing
- `cmd/stocd/` - Binary CLI entry point and testnet setup commands
- `x/stoc/` - Custom STOC module for token creation/management
  - `keeper/` - State management and message handlers
  - `types/` - Message types, errors, codecs
  - `ante/` - Tax enforcement (PostDecorator)
- `x/evmutil/` - EVM utilities including dual denomination conversion
  - `keeper/bank_keeper.go` - EvmBankKeeper with ustoc↔astoc conversion
- `proto/` - Protocol buffer definitions
- `api/` - Generated API code from protobuf (do not edit manually)
- `testutil/` - Test utilities and helpers

### Configuration Files

- `config.yml` - Ignite CLI configuration (accounts, validators, genesis)
- `go.mod` - Go dependencies with Cosmos SDK v0.53.4
- `proto/buf.yaml` - Protocol buffer configuration
- `Makefile` - Build, test, and lint targets

## Development Environment

- **Chain ID**: `stoc`
- **Native Token**: `ustoc` (6 decimals, Cosmos-side)
- **EVM Token**: `astoc` (18 decimals, EVM-side)
- **Coin Type**: 118 (Cosmos standard)
- **Local RPC**: http://localhost:26657
- **Local REST API**: http://localhost:1317
- **Local gRPC**: http://localhost:9090

## Network Information

- **Mainnet RPC**: https://rpc-stoc-mainnet.stochainscan.io/
- **Mainnet REST**: https://api-stoc-mainnet.stochainscan.io
- **Minimum Gas Price**: 0.001ustoc (mainnet), 0.0001ustoc (development)

## Governance History

| Proposal Hash                                                      | Mô tả               | Status      |
| ------------------------------------------------------------------ | ------------------- | ----------- |
| `725329CCB4525FD972E29B5F991F63055AA4C675218581472D0F86C3BC238EDE` | Giảm inflation rate | ✅ Approved |

### Chi tiết Proposal: Giảm Inflation Rate

**Thay đổi Mint Params:**

| Parameter     | Genesis (trước) | Sau proposal |
| ------------- | --------------- | ------------ |
| Inflation Max | 20%             | 0.0003%      |
| Inflation Min | 7%              | 0.00003%     |

**Tác động (với Total Supply ~10,000,030 STOC):**

|              | Trước      | Sau |
| ------------ | ---------- | --- |
| Max STOC/năm | ~2,000,000 | ~30 |
| Min STOC/năm | ~700,000   | ~3  |

→ Giảm **~99.99%** lượng token mới được mint mỗi năm.

## Custom Modules

### x/stoc Module

Module chính cho STOC blockchain:

- **Token Management**: Quản lý token securities
- **Burn**: Cho phép burn token - module burn đã được implement

## EVM Integration Architecture

### Dual Denomination System

The chain uses a dual denomination system implemented in `x/evmutil/keeper/bank_keeper.go`:

- **Cosmos Side**: `ustoc` with 6 decimals (standard Cosmos SDK)
- **EVM Side**: `astoc` with 18 decimals (Ethereum standard)
- **Conversion**: `EvmBankKeeper` automatically converts between denominations
- **Custom Tokens**: Tokens created via `x/stoc` module are **Cosmos-only** and NOT accessible from EVM

### EVM Modules and Keepers

Located in `app/evm.go`:

- **EVMKeeper**: Core EVM execution and state management
- **FeeMarketKeeper**: EIP-1559 dynamic fee market
- **Erc20Keeper**: ERC20 token bridge between Cosmos and EVM
- **EvmutilKeeper**: Custom utilities for dual denomination

### Custom Precompiles

Registered in `app/evm.go:postRegisterEVMModules()`:

- **Bech32 Precompile**: Convert between Ethereum and Cosmos addresses
- **P256 Precompile**: secp256r1 signature verification (EIP-7212)

### Custom EVM Activators

Gas multipliers for specific opcodes (defined in `app/evm.go:getCustomEVMActivators()`):

- CREATE/CREATE2: 10x multiplier
- CALL: 10x multiplier
- SSTORE: Fixed 2100 gas (EIP-2929 warm access cost)

### Ante Handlers

Located in `app/ante/`:

- `ante.go`: Router between Cosmos and EVM ante handlers
- `cosmos_handler.go`: Standard Cosmos SDK transaction validation
- `evm_handler.go`: EVM-specific transaction handling
- Tax enforcement is in `x/stoc/ante/tax_post.go` as a PostDecorator

## Custom Token System (`x/stoc` module)

### Token Features

Defined in `proto/stoc/stoc/token.proto`:

- **Token Metadata**: Name, symbol, decimals, logo
- **Supply Management**: Initial supply, total supply, remaining supply, unlimited flag
- **Wallet Distribution**: Initial token distribution to multiple addresses
- **Tax System**: Configurable transaction tax with recipient address
- **Creator Tracking**: Records token creator address
- **Minimal Denom**: Internal denomination (e.g., `MYTOKEN_0`)

### Token Operations

Message handlers in `x/stoc/keeper/`:

- `MsgCreateToken`: Create new fungible tokens with initial distribution
- `MsgMintTokens`: Mint additional tokens (if unlimited)
- `MsgReleaseTokens`: Release minted tokens to circulation
- `MsgBurnToken`: Burn tokens from circulation

### Token Storage

- Tokens stored by ID (sequential counter) in keeper
- Query by ID or symbol supported via `x/stoc/keeper/query_token.go`
- Token counter tracks next available ID

### Tax System

Implemented in `x/stoc/ante/tax_post.go`:

- Applied as PostDecorator after transaction success
- Tax deducted from recipient, not sender
- Only applies to `MsgSend` transactions
- Configurable percentage and recipient address per token
- Minimum tax amount is 1 if percentage rounds to zero

## Code Generation Workflow

This project uses protobuf-first development. When modifying `.proto` files:

1. Update protobuf definitions in `proto/stoc/`
2. Run `make proto-gen` or `ignite generate proto-go --yes`
3. Generated code appears in `api/` and module type directories
4. Run tests with `make test` before committing

## Module Structure Patterns

### Standard Cosmos SDK Module (`x/stoc`)

- `keeper/` - State management, business logic, message handlers
  - `keeper.go` - Main keeper struct with store access
  - `msg_server_*.go` - Individual message handler implementations
  - `query_*.go` - Query handlers for gRPC/REST API
  - `token.go` - Token storage operations (Get/Set/Delete)
- `types/` - Message types, errors, codecs, protobuf types
  - `keys.go` - Store keys and constants
  - `msg_*.go` - Message validation and routing
  - `errors.go` - Custom error definitions
- `ante/` - Transaction ante/post handlers (tax enforcement)
- `module/` - Module interface, genesis, autocli configuration
  - `module.go` - Module implementation
  - `genesis.go` - Genesis state initialization
  - `autocli.go` - Automatic CLI generation config

### Custom EVM Utilities (`x/evmutil`)

- `keeper/` - EvmBankKeeper with denomination conversion
- `types/` - Expected keepers, errors, keys
- `module.go` - Module registration

## Testing Framework

- Uses standard Go testing with testify/require
- Simulation tests in `app/sim_test.go` for complex scenarios
- Module-specific tests in `x/stoc/keeper/*_test.go`
- Test utilities in `testutil/` for keeper setup and sample data
- Always run `make test` before commits to ensure quality

## Linting & Quality

- Uses golangci-lint v1.61.0 with 15-minute timeout
- Additional security scanning with govulncheck
- Auto-fix available with `make lint-fix`
- Mandatory before commits

## Important Development Notes

### EVM and Custom Tokens

- **Custom tokens created via `x/stoc` are NOT accessible from EVM**
- Only native `ustoc`/`astoc` is available on EVM side
- This restriction is enforced in `x/evmutil/keeper/bank_keeper.go`
- Custom tokens require Cosmos SDK transactions (not Ethereum transactions)

### Denomination Conversion

- Conversion logic in `x/evmutil/keeper/bank_keeper.go`
- `ustoc` (6 decimals) ↔ `astoc` (18 decimals)
- Conversion factor: 1 ustoc = 10^12 astoc
- EvmBankKeeper wraps regular BankKeeper with automatic conversion

### Account Compatibility

- Uses coin type 118 (Cosmos standard) for backward compatibility
- Ethereum-style addresses (0x...) and Cosmos-style (stoc...) both supported
- Bech32 precompile helps convert between address formats

### Go Module Replacements

Important replacements in `go.mod`:

- `github.com/ethereum/go-ethereum` replaced with `github.com/cosmos/go-ethereum` for Cosmos EVM compatibility
- `github.com/syndtr/goleveldb` replaced with fixed version
- These are required for proper EVM integration

## Important Work Guidelines

**Critical Rules:**

- **NEVER automatically run build and lint** after completing a request
- **Always use Vietnamese** in conversation and notes with users
- **Only use English** in code and code comments

## File Encoding Guidelines

When writing Vietnamese in files, ensure UTF-8 encoding is correct to avoid character errors. Use proper Vietnamese diacritics but pay attention to file format when using Write tool.
