# STOC Chain - Node Setup Guide

Complete guide for running a STOC Chain node (full node, validator, or indexer).

## System Requirements

### Minimum (Full Node)

- CPU: 4 cores
- RAM: 16GB
- Storage: 500GB SSD
- Network: 100 Mbps
- OS: Ubuntu 20.04+ / CentOS 8+ / RHEL 8+

### Recommended (Validator / Indexer)

- CPU: 8+ cores
- RAM: 32GB+
- Storage: 1TB+ NVMe SSD
- Network: 1 Gbps
- OS: Ubuntu 22.04 LTS

## Network Information

| Parameter | Value |
|-----------|-------|
| Chain ID | `stoc` |
| Native Token | `ustoc` (6 decimals) |
| EVM Token | `astoc` (18 decimals) |
| Min Gas Price | `0.001ustoc` |
| RPC | https://rpc-stoc-mainnet.stochainscan.io/ |
| REST API | https://api-stoc-mainnet.stochainscan.io |
| Genesis | https://rpc-stoc-mainnet.stochainscan.io/genesis |
| Snapshot | https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest |
| Addrbook | https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook |
| Explorer | https://stochainscan.io |

## 1. Install Prerequisites

```bash
# Install Go 1.24.3
sudo apt update
sudo wget https://go.dev/dl/go1.24.3.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.24.3.linux-amd64.tar.gz

echo 'export GOROOT=/usr/local/go' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOROOT/bin:$GOPATH/bin:/usr/local/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
# go version go1.24.3 linux/amd64

# Install Buf CLI (for protobuf generation, optional)
go install github.com/bufbuild/buf/cmd/buf@latest
```

## 2. Build the Binary

### Option A: Build from Source

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

# Build with Ignite CLI
ignite chain build

# Or build with Make
make install

# Verify
stocd version
```

### Option B: Download Pre-built Binary

Download from [Releases](https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet/releases) or [Google Drive](https://drive.google.com/file/d/1FWjVfsqQ7Y6qR1U2aAIzk0pm08spMiq-/view?usp=sharing).

## 3. Initialize Node

```bash
# Initialize with your moniker name
stocd init <your_moniker_name> --chain-id stoc

# Set minimum gas price
sed -i 's/minimum-gas-prices = ""/minimum-gas-prices = "0.001ustoc"/' ~/.stoc/config/app.toml
```

## 4. Download Genesis

```bash
curl -s https://rpc-stoc-mainnet.stochainscan.io/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json

# Verify
stocd genesis validate ~/.stoc/config/genesis.json
```

## 5. Configure Peers

```bash
# Fetch peers from addrbook
curl -s https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook | jq -r '.data.addrs[] | "\(.addr.id)@\(.addr.ip):\(.addr.port)"' | head -10
```

Edit `~/.stoc/config/config.toml`:

```toml
persistent_peers = "4ed01c03afcca1399467c644efbb7f076cb406d0@202.182.110.150:26656,41f8094cd1da001a7a4416246c3ea5ab62196bd9@45.32.180.48:26656,5f0cd810689cc8907aa3520a75705b20f9f179bb@64.176.4.207:26656"
```

## 6. Sync with Snapshot (Recommended)

Using a snapshot speeds up sync from days to minutes.

```bash
# Stop node if running
sudo systemctl stop stocd

# Remove old data (keep config)
rm -rf ~/.stoc/data

# Download and extract snapshot
cd ~/.stoc
wget -O snapshot.tar.gz https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
tar -xzf snapshot.tar.gz
rm snapshot.tar.gz

# Fix permissions
chown -R $(whoami):$(whoami) ~/.stoc/data
```

## 7. Configure EVM (JSON-RPC)

Edit `~/.stoc/config/app.toml` to enable EVM JSON-RPC:

```toml
[evm]
tracer = ""

[json-rpc]
enable = true
address = "127.0.0.1:8545"  # Use "0.0.0.0:8545" only with proper firewall/ACL rules
ws-address = "127.0.0.1:8546"
allow-unprotected-txs = false
enable-unsafe = false
api = ["eth", "net", "web3", "txpool"]
```

**Warning**: Setting `address` to `0.0.0.0:8545` exposes JSON-RPC on all interfaces. Only do this explicitly with appropriate firewall/ACL rules in production. The default `127.0.0.1:8545` restricts access to localhost — use a reverse proxy to expose it publicly.

## 8. Start the Node

### Direct Start

```bash
stocd start
```

### Systemd Service (Recommended for Production)

Create a dedicated system user and install the binary:

```bash
# Create system user for running the node
sudo useradd -m -s /bin/bash stoc

# Copy built binary to system path
sudo cp $(go env GOPATH)/bin/stocd /usr/local/bin/
sudo chown root:root /usr/local/bin/stocd

# Copy initialized data to the stoc user's home (stocd init writes to ~/.stoc)
sudo cp -r ~/.stoc /home/stoc/.stoc

# Ensure data directory is owned by stoc user
sudo chown -R stoc:stoc /home/stoc/.stoc
```

Create `/etc/systemd/system/stocd.service`:

```ini
[Unit]
Description=STOC Chain Node
After=network-online.target

[Service]
User=stoc
ExecStart=/usr/local/bin/stocd start
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable stocd
sudo systemctl start stocd

# Check status
sudo systemctl status stocd
```

## 9. Verify Sync

```bash
# Check if still catching up
stocd status | jq '.SyncInfo.catching_up'
# false = fully synced

# Check current block height
stocd status | jq '.SyncInfo.latest_block_height'

# Test EVM JSON-RPC
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:8545
```

---

## Become a Validator

After your node is fully synced:

### 1. Create or Import a Key

```bash
# Create new key
stocd keys add <key_name>

# Or import existing
stocd keys add <key_name> --recover
```

### 2. Fund Your Account

Transfer at least 1,000,000 ustoc (1 STOC) to your validator address for self-delegation plus gas fees.

### 3. Create Validator

```bash
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

### 4. Verify Validator

```bash
# Check your validator
stocd query staking validator $(stocd keys show <key_name> --bech val -a)

# Check all validators
stocd query staking validators
```

---

## Run as Indexer Node

An indexer node stores all transaction data and serves API queries. Configuration differs from a validator:

### Indexer-Specific Configuration

Edit `~/.stoc/config/config.toml`:

```toml
# Index all events for query
indexer = "kv"
```

Edit `~/.stoc/config/app.toml`:

```toml
# Enable API
[api]
enable = true
swagger = true
address = "tcp://0.0.0.0:1317"

[grpc]
enable = true
address = "0.0.0.0:9090"

# Enable EVM JSON-RPC
[json-rpc]
enable = true
address = "0.0.0.0:8545"
ws-address = "0.0.0.0:8546"
api = ["eth", "net", "web3", "txpool"]

# Keep all state (no pruning)
pruning = "nothing"
```

### Ports Reference

| Port | Protocol | Purpose |
|------|----------|---------|
| 26656 | TCP | P2P (required) |
| 26657 | TCP | Tendermint RPC |
| 1317 | TCP | REST API |
| 9090 | TCP | gRPC |
| 8545 | TCP | EVM JSON-RPC |
| 8546 | TCP | EVM WebSocket |

### Firewall Setup

#### Validator Nodes (Ubuntu/Debian — UFW)

```bash
# P2P only (validators should NOT expose API/RPC publicly)
sudo ufw allow 26656/tcp
sudo ufw enable
```

#### Indexer / Public Nodes (Ubuntu/Debian — UFW)

```bash
# P2P (required)
sudo ufw allow 26656/tcp

# RPC/API (for public-facing nodes)
sudo ufw allow 26657/tcp
sudo ufw allow 1317/tcp
sudo ufw allow 9090/tcp
sudo ufw allow 8545/tcp
sudo ufw allow 8546/tcp

sudo ufw enable
```

#### CentOS / RHEL (firewalld)

```bash
# Validator: P2P only
sudo firewall-cmd --permanent --add-port=26656/tcp
sudo firewall-cmd --reload

# Indexer / Public: add API/RPC ports
sudo firewall-cmd --permanent --add-port=26657/tcp
sudo firewall-cmd --permanent --add-port=1317/tcp
sudo firewall-cmd --permanent --add-port=9090/tcp
sudo firewall-cmd --permanent --add-port=8545/tcp
sudo firewall-cmd --permanent --add-port=8546/tcp
sudo firewall-cmd --reload
```

---

## Monitoring

```bash
# Node status
stocd status | jq

# Sync progress
stocd status | jq '.SyncInfo'

# Peer count
stocd net_info | jq '.n_peers'

# Account balance
stocd query bank balances <address>

# View logs (systemd)
sudo journalctl -u stocd -f
sudo journalctl -u stocd -n 100 --no-pager
sudo journalctl -u stocd | grep -i error
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Peer connection failed | Update peers from addrbook API |
| Genesis mismatch | Re-download genesis file |
| Disk full | Expand storage or enable pruning |
| Out of memory | Increase RAM or add swap |
| JSON-RPC not responding | Check `app.toml` `[json-rpc]` section, verify port not blocked |
| Slow sync | Use snapshot instead of syncing from genesis |

## Security Best Practices

- Keep node binary updated to latest version
- Never share validator private keys (`priv_validator_key.json`)
- Use firewall to restrict access to necessary ports only
- Run node under a dedicated system user (not root)
- Regular backup of `~/.stoc/config/` and validator keys
- Monitor node uptime — missed blocks result in slashing

## Upgrade Process

When a chain upgrade is announced via governance proposal:

1. Check the upgrade name and target height
2. Build or download the new binary
3. Replace the old binary before the upgrade height
4. The chain halts at the upgrade height, runs upgrade handler, then resumes

```bash
# Check active upgrade plan
stocd query upgrade plan

# Check if upgrade was applied
stocd query upgrade applied <upgrade-name>
```

For automated upgrades, consider using [Cosmovisor](https://docs.cosmos.network/main/build/tooling/cosmovisor).
