# STOC Blockchain Documentation

Welcome to the comprehensive documentation for STOC Blockchain. This repository contains all the necessary guides and references for developers, node operators, and users.

## 📚 Documentation Index

### 🔗 Chain Setup and Operation
- **[Chain Setup Guide](./chain/readme.md)** - Complete guide for setting up and running a STOC blockchain node
  - Fullnode setup (10k blocks via snapshot — recommended)
  - Full archive node (ALL blocks from genesis)
  - Become a validator
  - systemd service, firewall, monitoring

### 🛠️ Development Resources
- **[Development Guide](./development/readme.md)** - Comprehensive development documentation
  - Development environment setup
  - Module development
  - Testing frameworks
  - Code standards and best practices
  - Frontend integration

## 🏗️ STOC Blockchain Overview

STOC Blockchain is a high-performance blockchain built using the Cosmos SDK and Ignite CLI. It provides:

- **Fast Transactions**: Sub-second finality
- **Low Fees**: Minimum gas price of 0.001ustoc
- **EVM Compatibility**: Full Ethereum Virtual Machine support via Cosmos EVM v0.6.0 — deploy Solidity contracts, connect MetaMask, use Web3.js / ethers.js
- **Dual Denomination**: `ustoc` (6 decimals, Cosmos) ↔ `astoc` (18 decimals, EVM) with automatic conversion
- **Custom Token System**: Create fungible tokens with configurable tax via `x/stoc` module
- **Interoperability**: IBC-enabled for cross-chain communication
- **Developer Friendly**: Comprehensive tooling and documentation
- **Secure**: Built on proven Tendermint consensus

## 🔧 System Requirements

### Minimum Requirements
- **CPU**: 4 cores
- **RAM**: 16GB
- **Storage**: 500GB SSD
- **Network**: 100 Mbps
- **OS**: Ubuntu 20.04+ / CentOS 8+ / RHEL 8+

### Recommended for Production
- **CPU**: 8+ cores
- **RAM**: 32GB+
- **Storage**: 1TB+ NVMe SSD
- **Network**: 1 Gbps
- **OS**: Ubuntu 22.04 LTS

## 🌐 Network Information

### Mainnet
- **Chain ID**: `stoc`
- **Native Denom**: `ustoc` (6 decimals)
- **EVM Denom**: `astoc` (18 decimals)
- **Minimum Gas Price**: `0.001ustoc`
- **REST API**: `https://api-stoc-mainnet.stochainscan.io`
- **RPC**: `https://rpc-stoc-mainnet.stochainscan.io/`
- **Genesis**: `https://rpc-stoc-mainnet.stochainscan.io/genesis`
- **Snapshot**: `https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest`

### Development
- **Local REST API**: `http://localhost:1317`
- **Local RPC**: `http://localhost:26657`
- **Local gRPC**: `http://localhost:9090`
- **Local EVM JSON-RPC**: `http://localhost:8545`
- **Local EVM WebSocket**: `http://localhost:8546`

## 📋 Prerequisites

### For All Users
- Go 1.24.3+ (toolchain go1.24.3)
- Git
- Basic command line knowledge

### For Developers
- Ignite CLI
- Protocol Buffers compiler (protoc)

### For Production Deployment
- Linux server administration knowledge
- Understanding of blockchain concepts
- Network and security configuration experience

## 🔗 Important Links

- **Source Code**: [GitHub Repository](https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet)
- **Snapshot Service**: [Download Latest](https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest)
- **Addrbook API**: [Peer Discovery](https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook)
- **Block Explorer**: [STOC Chain Scan](https://stochainscan.io)

## 🤝 Contributing

We welcome contributions to improve the documentation and codebase:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

### Documentation Guidelines
- Use clear, concise language
- Include code examples where appropriate
- Test all commands and procedures
- Update the index when adding new documents

## 📞 Support

### Community Support
- **GitHub Issues**: [Report bugs or request features](https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet/issues)
- **Documentation Issues**: Create an issue for documentation improvements

### Professional Support
- Contact the STOC development team for enterprise support
- Custom integration and deployment services available

## 📄 License

This documentation is provided under the same license as the STOC Blockchain project. Please refer to the main repository for license details.

## 🔄 Updates

This documentation is regularly updated to reflect the latest features and best practices. Check the repository for the most current version.

### Recent Updates
- Added Ignite CLI build instructions
- Enhanced deployment guides for cloud providers
- Expanded API documentation with more examples
- Added comprehensive troubleshooting sections

---

**Note**: This documentation assumes familiarity with blockchain concepts and basic system administration. For beginners, we recommend starting with the Cosmos SDK documentation to understand the underlying technology.

## 📖 Additional Resources

- [Cosmos SDK Documentation](https://docs.cosmos.network/)
- [Ignite CLI Documentation](https://docs.ignite.com/)
- [Tendermint Documentation](https://docs.tendermint.com/)
- [IBC Protocol](https://ibc.cosmos.network/)

For the most up-to-date information, always refer to the official STOC Blockchain repository and this documentation. 