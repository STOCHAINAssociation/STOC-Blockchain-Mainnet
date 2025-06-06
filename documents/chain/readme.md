# STOC Blockchain Node Setup Guide

This guide provides step-by-step instructions for setting up and running a STOC blockchain node.

## Prerequisites

- Go 1.23.2 or higher (toolchain go1.24.3)
- Ignite CLI v29 (including Cosmos SDK)
- Git
- Minimum 16GB RAM
- 500GB+ available disk space
- Stable internet connection

## Important Links

- **Snapshot Download**: https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
- **Genesis JSON**: https://api-stoc-mainnet.stochainscan.io/rpc/genesis
- **Addrbook API (for peers)**: https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook
- **Source Code**: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet

## Setup Instructions

### 1. Build Chain

Clone the repository and build the binary (by using Ignite CLI):

```bash
# Clone the repository
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

# Build the binary
ignite chain build
```

Check if stocd is installed:
```bash
stocd version
```

### Alternative: Download Pre-built Binary

If you encounter issues building the chain, you can download the pre-built binary: [Here](https://drive.google.com/file/d/14XYPWSUPqs-JX5-wPMN7FtEA6B4_zv9a/view?usp=sharing)


### 2. Initialize Node

Initialize your node with a custom moniker name:

```bash
stocd init <your_moniker_name> --chain-id stoc
```

**Important Configuration:**
- Chain ID: `stoc`
- Minimum gas price: `0.001ustoc`

Update the minimum gas price in your configuration:
```bash
# Edit app.toml to set minimum gas prices
sed -i 's/minimum-gas-prices = ""/minimum-gas-prices = "0.001ustoc"/' ~/.stoc/config/app.toml
```

### 3. Configure Peers

Get the current peer list from the addrbook API and configure your node:

```bash
# Fetch peers from addrbook API
curl -s https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook | jq -r '.data.addrs[] | "\(.addr.id)@\(.addr.ip):\(.addr.port)"' | head -10

# Current active peers for STOC mainnet:
persistent_peers = "4ed01c03afcca1399467c644efbb7f076cb406d0@157.66.100.146:26656,41f8094cd1da001a7a4416246c3ea5ab62196bd9@157.66.101.49:26656,5f0cd810689cc8907aa3520a75705b20f9f179bb@103.161.180.115:26656"
```

### 4. Update Genesis

Download and update the genesis file:

```bash
# Download genesis file
curl -s https://api-stoc-mainnet.stochainscan.io/rpc/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json

# Verify genesis file
stocd validate-genesis ~/.stoc/config/genesis.json
```

### 5. Download Chain Data (Snapshot)

To speed up synchronization, download the latest snapshot:

```bash
# Stop the node if running
sudo systemctl stop stocd

# Remove old data
rm -rf ~/.stoc/data

# Download and extract snapshot
cd ~/.stoc
wget -O snapshot.tar.gz https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
tar -xzf snapshot.tar.gz
rm snapshot.tar.gz

# Set proper permissions
chown -R $(whoami):$(whoami) ~/.stoc/data
```

### 6. Start Node and Begin Sync

Start the STOC daemon to begin synchronization:

```bash
# Start node directly
stocd start
```

## Monitoring and Maintenance

### Check Sync Status

```bash
# Check if node is catching up
stocd status | jq '.SyncInfo.catching_up'

# Check current block height
stocd status | jq '.SyncInfo.latest_block_height'

# Check peer connections
stocd status | jq '.NodeInfo.network'
```

### Useful Commands

```bash
# Check node status
stocd status

# Check account balance
stocd query bank balances <address>

# Check validator info
stocd query staking validators

# Create validator (after sync completion)
stocd tx staking create-validator \
  --amount=1000000ustoc \
  --pubkey=$(stocd tendermint show-validator) \
  --moniker="<your_moniker>" \
  --chain-id=stoc \
  --commission-rate="0.05" \
  --commission-max-rate="0.20" \
  --commission-max-change-rate="0.01" \
  --min-self-delegation="1" \
  --gas="auto" \
  --gas-prices="0.001ustoc" \
  --from=<key_name>
```

## Troubleshooting

### Common Issues

1. **Peer Connection Issues**: Update peers list from the addrbook API
2. **Genesis Mismatch**: Re-download genesis file
3. **Disk Space**: Ensure sufficient disk space for blockchain data
4. **Memory Issues**: Increase system memory or add swap space

### Log Analysis

```bash
# View recent logs
sudo journalctl -u stocd -n 100

# Follow logs in real-time
sudo journalctl -u stocd -f

# Check for errors
sudo journalctl -u stocd | grep -i error
```

## Security Considerations

- Keep your node updated with the latest version
- Secure your private keys and never share them
- Use firewall to restrict access to necessary ports only
- Regular backup of your validator keys and important data
- Monitor your node's performance and uptime

## Support

For additional support and community discussions:
- GitHub Issues: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet/issues
- Official Documentation: Check the repository's docs folder

---

**Note**: This guide assumes a Linux environment. Adjust commands accordingly for other operating systems.
