# STOC Chain

STOC Chain is a high-performance blockchain with full EVM (Ethereum Virtual Machine) compatibility, built on Cosmos SDK v0.53.4 and CometBFT consensus.

## Features

- **EVM Compatible**: Deploy Solidity smart contracts, use MetaMask, Web3.js, ethers.js
- **Cosmos Native**: IBC transfers, staking, governance, bank module
- **Dual Denomination**: `ustoc` (6 decimals, Cosmos) / `astoc` (18 decimals, EVM) with automatic conversion
- **Custom Token System**: Create fungible tokens with configurable tax via `x/stoc` module
- **Custom Precompiles**: Bech32 address conversion, P256 signature verification (EIP-7212)

## Network Information

| Parameter | Mainnet | Development |
|-----------|---------|-------------|
| Chain ID | `stoc` | `stoc` |
| Native Token | `ustoc` (6 decimals) | `ustoc` |
| EVM Token | `astoc` (18 decimals) | `astoc` |
| Coin Type | 118 | 118 |
| Min Gas Price | `0.001ustoc` | `0.0001ustoc` |
| RPC | https://rpc-stoc-mainnet.stochainscan.io/ | http://localhost:26657 |
| REST API | https://api-stoc-mainnet.stochainscan.io | http://localhost:1317 |
| gRPC | Not publicly exposed | http://localhost:9090 |
| JSON-RPC (EVM) | Not publicly exposed | http://localhost:8545 |
| WebSocket (EVM) | Not publicly exposed | http://localhost:8546 |
| Block Explorer | https://stochainscan.io | — |

## Quick Start

### Development

```bash
# Clone and start with hot reload
git clone https://github.com/MinhAnh-Corp/stochain.git
cd stochain
ignite chain serve
```

### Build from Source

```bash
# Build binary
make install

# Verify
stocd version
```

### Run a Node

See [Node Setup Guide](./documents/chain/readme.md) for full instructions including snapshot sync, peer configuration, and validator setup.

## Technology Stack

| Component | Version |
|-----------|---------|
| Cosmos SDK | v0.53.6 |
| CometBFT | v0.38.21 |
| IBC | v10.5.0 |
| Cosmos EVM | v0.6.0 |
| Go | 1.24.3 |

## Architecture

```
stochain/
├── app/                    # Core application, EVM integration, ante handlers
│   ├── app.go              # Main app with dependency injection
│   ├── evm.go              # EVM module, precompiles, gas multipliers
│   └── ante/               # Cosmos + EVM ante handler routing
├── cmd/stocd/              # CLI binary entry point
├── x/stoc/                 # Custom token module (create, mint, burn, tax)
├── x/evmutil/              # EVM utilities (ustoc <-> astoc conversion)
├── proto/                  # Protocol buffer definitions
└── documents/              # Documentation
```

### EVM Integration

- **Dual Denomination**: `EvmBankKeeper` auto-converts `ustoc` (6 dec) ↔ `astoc` (18 dec), factor = 10^12
- **Custom Tokens**: Tokens created via `x/stoc` are Cosmos-only, NOT accessible from EVM
- **Gas Multipliers**: CREATE/CREATE2/CALL at 10x, SSTORE at 2100 gas (EIP-2929)
- **Precompiles**: Bech32 (address conversion), P256 (secp256r1 signatures)

### Custom Token System (`x/stoc`)

- Create tokens with metadata, supply management, initial distribution
- Configurable transaction tax (percentage + recipient)
- Mint, release, burn operations
- IBC transfers of custom tokens are blocked (native `ustoc` only)

## Documentation

| Document | Description |
|----------|-------------|
| [Node Setup Guide](./documents/chain/readme.md) | Run a full node, sync, become a validator |
| [Development Guide](./documents/development/readme.md) | Dev environment, module development, testing |
| [Documentation Index](./documents/README.md) | All documentation links |

## Build & Test Commands

```bash
make install        # Build and install stocd
make test           # Full test suite (vet + vuln + unit)
make test-unit      # Unit tests only
make test-race      # Tests with race detection
make test-cover     # Coverage report
make lint           # Run golangci-lint
make lint-fix       # Auto-fix lint issues
make proto-gen      # Regenerate protobuf code
```

## Source Code

- **GitHub**: https://github.com/MinhAnh-Corp/stochain
- **Mainnet Binary**: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet

## License

See [LICENSE](./LICENSE) for details.
