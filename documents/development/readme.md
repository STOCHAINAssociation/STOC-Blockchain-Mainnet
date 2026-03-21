# STOC Chain - Development Guide

Development documentation for building on and contributing to STOC Chain.

## Prerequisites

- Go 1.24+
- Ignite CLI
- Git

## Development Setup

```bash
# Clone
git clone https://github.com/MinhAnh-Corp/stochain.git
cd stochain

# Start with hot reload
ignite chain serve

# This builds, initializes with test accounts, and starts the chain locally
```

### Test Accounts (from config.yml)

| Account | Balance | Purpose |
|---------|---------|---------|
| admin | 10^16 ustoc | Admin operations |
| validator1 | 10^10 ustoc | Validator + faucet |
| validator2 | 10^10 ustoc | Second validator |

## Project Structure

```
stochain/
├── app/                        # Core application
│   ├── app.go                  # Main app (dependency injection)
│   ├── app_config.go           # Module configuration
│   ├── evm.go                  # EVM module, precompiles, gas config
│   ├── ibc.go                  # IBC module setup
│   ├── upgrades.go             # Chain upgrade handlers
│   └── ante/                   # Transaction ante/post handlers
│       ├── ante.go             # Cosmos/EVM router
│       ├── cosmos_handler.go   # Cosmos ante chain
│       └── evm_handler.go      # EVM ante chain
├── cmd/stocd/                  # CLI binary
│   └── cmd/
│       ├── root.go             # Root command
│       ├── commands.go         # Additional commands
│       ├── testnet.go          # Single-node testnet
│       └── testnet_multi_node.go
├── x/stoc/                     # Custom token module
│   ├── keeper/                 # State management
│   ├── types/                  # Message types, errors
│   ├── ante/                   # Tax post-decorator, IBC restriction
│   ├── module/                 # Module interface
│   └── simulation/             # Simulation helpers
├── x/evmutil/                  # EVM denomination conversion
│   ├── keeper/bank_keeper.go   # ustoc <-> astoc conversion
│   └── types/
├── proto/stoc/                 # Protobuf definitions
├── api/                        # Generated API (do NOT edit)
├── testutil/                   # Test utilities
└── documents/                  # Documentation
```

## Build Commands

```bash
make install        # Build and install stocd binary
make test           # Full test suite (vet + vuln + unit)
make test-unit      # Unit tests only
make test-race      # Tests with race detection
make test-cover     # Generate coverage report
make lint           # Run golangci-lint (15min timeout)
make lint-fix       # Auto-fix lint issues
make proto-gen      # Regenerate protobuf code
```

### Run a Single Test

```bash
go test -mod=readonly -v -timeout 30m ./path/to/package -run TestFunctionName
```

## Module Development

### Scaffold with Ignite CLI

```bash
# New module
ignite scaffold module <name>

# New message
ignite scaffold message <name> <field1> <field2> --module <module>

# New query
ignite scaffold query <name> <field1> --module <module>
```

### Protobuf Workflow

1. Define messages in `proto/stoc/<module>/`
2. Run `make proto-gen` or `ignite generate proto-go --yes`
3. Generated code appears in `api/` and `x/<module>/types/`
4. Run `make test` before committing

### Keeper Pattern

```go
// x/<module>/keeper/keeper.go
type Keeper struct {
    cdc          codec.BinaryCodec
    storeService store.KVStoreService
    // ...
}

// x/<module>/keeper/msg_server_*.go
func (k msgServer) HandleMessage(ctx context.Context, msg *types.MsgX) (*types.MsgXResponse, error) {
    // ValidateBasic already called by ante handler
    // Business logic here
    return &types.MsgXResponse{}, nil
}
```

## EVM Development

### Architecture

- **Dual Denomination**: `ustoc` (6 dec, Cosmos) ↔ `astoc` (18 dec, EVM)
- **Conversion**: 1 ustoc = 10^12 astoc, handled by `EvmBankKeeper`
- **Custom Tokens**: Cosmos-only, NOT accessible from EVM side
- **Gas Multipliers**: CREATE/CREATE2/CALL at 10x; SSTORE fixed cost: 2100 gas (EIP-2929 warm access)

### Test EVM Locally

```bash
# Start chain
ignite chain serve

# Test JSON-RPC
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:8545

# Get block number
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  http://localhost:8545
```

### Deploy Smart Contracts

Use standard Ethereum tools (Hardhat, Foundry, Remix) pointing to `http://localhost:8545`.

```bash
# Foundry example
forge create --rpc-url http://localhost:8545 \
  --private-key <PRIVATE_KEY> \
  src/MyContract.sol:MyContract
```

### Custom Precompiles

Registered in `app/evm.go:postRegisterEVMModules()`:

| Precompile | Purpose |
|------------|---------|
| Bech32 | Convert between 0x and stoc1... addresses |
| P256 | secp256r1 signature verification (EIP-7212) |

## Custom Token System (`x/stoc`)

### Token Operations

| Message | Description |
|---------|-------------|
| `MsgCreateToken` | Create new token with metadata, supply, distributions |
| `MsgMintTokens` | Mint additional tokens (if unlimited flag set) |
| `MsgReleaseTokens` | Release minted tokens to circulation |
| `MsgBurnToken` | Burn tokens from circulation |

### Tax System

- Applied as `PostDecorator` after transaction success
- Tax deducted from recipient, not sender
- Only applies to `MsgSend` transactions
- Configurable percentage and recipient per token
- Minimum tax: 1 unit (if percentage rounds to zero)

### IBC Restrictions

- Custom tokens created via `x/stoc` are blocked from IBC transfers
- Only native `ustoc` can be transferred via IBC
- Enforced by `IBCCustomTokenRestriction` ante decorator

## Frontend Integration

### CosmJS

```typescript
import { SigningStargateClient } from "@cosmjs/stargate";

const client = await SigningStargateClient.connectWithSigner(
  "http://localhost:26657",
  signer
);

// Query balance
const balance = await client.getBalance(address, "ustoc");

// Send tokens
await client.sendTokens(sender, recipient, [{ denom: "ustoc", amount: "1000000" }], fee);
```

### EVM (ethers.js / Web3.js)

```typescript
import { ethers } from "ethers";

const provider = new ethers.JsonRpcProvider("http://localhost:8545");
const balance = await provider.getBalance(address);
```

## API Endpoints

| Endpoint | Port | Protocol |
|----------|------|----------|
| Tendermint RPC | 26657 | HTTP/WS |
| REST API | 1317 | HTTP |
| gRPC | 9090 | gRPC |
| EVM JSON-RPC | 8545 | HTTP |
| EVM WebSocket | 8546 | WS |
| OpenAPI Docs | 1317/swagger | HTTP |

## Code Quality

- golangci-lint v1.64.8 with 15-minute timeout
- govulncheck for security scanning
- Always run `make test` before commits
- Follow conventional commits: `feat(module):`, `fix(keeper):`, `chore(proto):`

## Debugging

```bash
# Debug mode
stocd start --log_level debug

# Delve debugger
dlv debug ./cmd/stocd -- start

# Check transaction
stocd query tx <hash>

# Check account
stocd query bank balances <address>
```
