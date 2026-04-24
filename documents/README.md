# STOC Chain Documentation

## Guides

- **[Node Setup Guide](./chain/readme.md)** — Run a full node, sync from snapshot, become a validator, run an indexer node
- **[Development Guide](./development/readme.md)** — Dev environment, module development, EVM integration, testing

## API Reference

- **[OpenAPI Spec](../docs/static/openapi.json)** — Auto-generated REST API documentation

## Network Information

| Parameter | Value |
|-----------|-------|
| Chain ID | `stoc` |
| Native Token | `ustoc` (6 decimals) |
| EVM Token | `astoc` (18 decimals) |
| Min Gas Price | `0.001ustoc` |
| RPC | https://rpc-stoc-mainnet.stochainscan.io/ |
| REST API | https://api-stoc-mainnet.stochainscan.io |
| Explorer | https://stochainscan.io |
| Snapshot | https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest |

## System Requirements

### Minimum (Full Node)
- CPU: 4 cores, RAM: 16GB, Storage: 500GB SSD, Network: 100 Mbps

### Recommended (Validator / Indexer)
- CPU: 8+ cores, RAM: 32GB+, Storage: 1TB+ NVMe SSD, Network: 1 Gbps

## External Resources

- [Cosmos SDK Documentation](https://docs.cosmos.network/)
- [Ignite CLI Documentation](https://docs.ignite.com/)
- [Cosmos EVM](https://github.com/cosmos/evm)
- [IBC Protocol](https://ibc.cosmos.network/)
- [Source Code](https://github.com/MinhAnh-Corp/stochain)
