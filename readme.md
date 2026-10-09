# STOChain

STOChain is a high-performance blockchain with full EVM (Ethereum Virtual Machine) compatibility, built on Cosmos SDK v0.53.6 and CometBFT consensus.

> **Current release: `v5.0.1`** — security patch backporting the upstream cosmos/evm v0.6.3 fix for GHSA-367m-g444-9mg3 ("non-atomic StateDB commit"). App-hash-identical to v5.0.0. Node operators: upgrade with a simple binary swap (no halt). See [HISTORY.md](./HISTORY.md).

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
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet
ignite chain serve
```

### Build from Source (release node)

```bash
# Clone the public repository and check out the current release tag
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet
git checkout v5.0.1

# Build binary (Go only — no buf/protoc needed; proto is generated + committed)
make install

# Verify
stocd version
```

### Source by Phase

`main` is the current release, `v5.0.1`. Each earlier binary that ran on mainnet has a tag and a branch, kept so the chain can be replayed from genesis. [HISTORY.md](./HISTORY.md) gives the exact block ranges and the replay order.

| Branch | Tag | Mainnet blocks | Go |
|---|---|---|---|
| [`phase/v1`](../../tree/phase/v1) | `v1` | 1 – 542,404 | 1.24.3 |
| [`phase/v1.1`](../../tree/phase/v1.1) | `v1.1` | 542,405 – 2,709,241 | 1.24.3 |
| [`phase/v1.2`](../../tree/phase/v1.2) | `v1.2` | 2,709,242 – 4,455,466 | 1.24.3 |
| [`phase/v2-evm`](../../tree/phase/v2-evm) | `v2-evm` | 4,455,467 – 4,699,537 | 1.24.3 |
| [`phase/v2-evm-tail`](../../tree/phase/v2-evm-tail) | `v2-evm-tail` | 4,699,538 – 4,705,315 | 1.24.3 |
| [`phase/v3`](../../tree/phase/v3) | `v3` | 4,705,316 – 4,794,076 | 1.24.3 |
| [`phase/v3.1`](../../tree/phase/v3.1) | `v3.1` | 4,794,077 – 6,408,099 | 1.24.3 |
| [`phase/v5.0.0`](../../tree/phase/v5.0.0) | `v5.0.0` | 6,408,100 – head | 1.25.8 |
| `main` | `v5.0.1` | same as `v5.0.0` (app-hash-identical) | 1.25.8 |

### Run a Node

1. Build `stocd` as above and run `stocd init <moniker> --chain-id stoc`.
2. Fetch the genesis file from the public RPC:
   `curl -s https://rpc-stoc-mainnet.stochainscan.io/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json`
3. In `~/.stoc/config/app.toml` set `minimum-gas-prices = "0.001ustoc"` and, under `[evm]`, `evm-chain-id = 1306`.
4. Either restore the latest snapshot, or replay from block 1 by following [HISTORY.md](./HISTORY.md).

## Technology Stack

| Component | Version |
|-----------|---------|
| Cosmos SDK | v0.53.6 |
| CometBFT | v0.38.21 |
| IBC | v10.5.0 |
| Cosmos EVM | v0.6.0 (in-tree fork; v0.6.3 security fix backported) |
| Go | 1.25.8 |

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
└── proto/                  # Protocol buffer definitions
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
| [HISTORY.md](./HISTORY.md) | Binary history from genesis, replay instructions, verification |

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

- **GitHub**: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet

## Technical Support

For technical questions about running a node or building from source, contact dev.minhanhcorp@gmail.com.

## License

See [LICENSE](./LICENSE) for details.
