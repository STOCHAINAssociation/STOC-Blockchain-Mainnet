# STOC Blockchain Node Setup Guide

This guide covers three setup scenarios for STOC mainnet:

1. **Fullnode (10k blocks)** — Quick setup using snapshot, recommended for most users
2. **Fullnode (ALL blocks)** — Full archive node, stores entire blockchain history
3. **Become a Validator** — Join the active validator set

## Prerequisites

- **OS**: Ubuntu 22.04+ LTS
- **Go**: 1.24.3+
- **RAM**: 8GB minimum (16GB+ recommended for validators)
- **Disk**: 50GB for 10k blocks / 500GB+ for ALL blocks
- **Network**: Stable connection, 100 Mbps+

## Important Links

| Resource | URL |
|----------|-----|
| Snapshot Download | https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest |
| Genesis JSON | https://rpc-stoc-mainnet.stochainscan.io/genesis |
| Addrbook (peers) | https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook |
| RPC Endpoint | https://rpc-stoc-mainnet.stochainscan.io |
| REST API | https://api-stoc-mainnet.stochainscan.io |
| gRPC | https://grpc-stoc-mainnet.stochainscan.io |
| Block Explorer | https://stochainscan.io |
| Source Code | https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet |

## Chain Info

| Key | Value |
|-----|-------|
| Chain ID | `stoc` |
| Coin Type | 118 (Cosmos standard) |
| Native Denomination | `ustoc` (6 decimals, Cosmos) |
| EVM Denomination | `astoc` (18 decimals, EVM) |
| Conversion | 1 `ustoc` = 10^12 `astoc` |
| Minimum Gas Price | `0.001ustoc` |
| Binary | `stocd` |
| EVM Compatibility | Cosmos EVM v0.6.0 (Solidity, MetaMask, ethers.js) |

---

## Common Setup (All Scenarios)

### 1. Install Go

```bash
sudo apt update && sudo apt install -y build-essential jq curl wget

# Install Go 1.24.3
wget https://go.dev/dl/go1.24.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.3.linux-amd64.tar.gz
rm go1.24.3.linux-amd64.tar.gz

echo 'export GOROOT=/usr/local/go' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOROOT/bin:$GOPATH/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
```

### 2. Build stocd

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

# Option A: Using make (recommended)
make install

# Option B: Using Ignite CLI (requires Ignite v29+)
ignite chain build

# Verify
stocd version
```

> **Alternative**: If you cannot build from source, download the pre-built binary from [here](https://drive.google.com/file/d/1FWjVfsqQ7Y6qR1U2aAIzk0pm08spMiq-/view?usp=sharing).

### 3. Initialize Node

```bash
stocd init <your_moniker_name> --chain-id stoc
```

### 4. Download Genesis

```bash
curl -s https://rpc-stoc-mainnet.stochainscan.io/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json

# Verify
stocd genesis validate ~/.stoc/config/genesis.json
```

### 5. Configure Node

```bash
# Set minimum gas price
sed -i 's/minimum-gas-prices = ""/minimum-gas-prices = "0.001ustoc"/' ~/.stoc/config/app.toml

# Set persistent peers
PEERS="5f0cd810689cc8907aa3520a75705b20f9f179bb@64.176.4.207:26656,4ed01c03afcca1399467c644efbb7f076cb406d0@202.182.110.150:26656,41f8094cd1da001a7a4416246c3ea5ab62196bd9@45.32.180.48:26656,4310f4113afd1cc4c220ea764f6d4710e1616b84@160.191.50.204:26656"
sed -i "s/persistent_peers = \"\"/persistent_peers = \"$PEERS\"/" ~/.stoc/config/config.toml
```

> You can also fetch peers dynamically from the addrbook API:
> ```bash
> curl -s https://api-sync-stoc-mainnet.stochainscan.io/snapshots/addrbook | jq -r '.data.addrs[] | "\(.addr.id)@\(.addr.ip):\(.addr.port)"' | head -10
> ```

### 6. Setup systemd Service

```bash
sudo tee /etc/systemd/system/stocd.service > /dev/null <<EOF
[Unit]
Description=STOC Blockchain Daemon
After=network-online.target

[Service]
User=root
ExecStart=$(which stocd) start
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable stocd
```

---

## Scenario 1: Fullnode (10k blocks) — Using Snapshot

Recommended for most users. Uses a snapshot to skip syncing from genesis.

### Set Pruning Config

```bash
sed -i 's/pruning = "default"/pruning = "custom"/' ~/.stoc/config/app.toml
sed -i 's/pruning-keep-recent = "0"/pruning-keep-recent = "10000"/' ~/.stoc/config/app.toml
sed -i 's/pruning-interval = "0"/pruning-interval = "100"/' ~/.stoc/config/app.toml
```

### Download and Apply Snapshot

```bash
# Remove default data
rm -rf ~/.stoc/data

# Download and extract snapshot
cd ~/.stoc
wget -O snapshot.tar.gz https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
tar -xzf snapshot.tar.gz
rm snapshot.tar.gz
```

### Start Node

```bash
sudo systemctl start stocd

# Check logs
sudo journalctl -u stocd -f
```

The node will sync remaining blocks from the snapshot height to the current height. This typically takes a few minutes.

---

## Scenario 2: Fullnode (ALL blocks) — Full Archive

Stores the entire blockchain history from block 1. Requires more disk space and time to sync.

### Set Pruning Config

```bash
sed -i 's/pruning = "default"/pruning = "nothing"/' ~/.stoc/config/app.toml
```

### Start Node (Sync from Genesis)

```bash
sudo systemctl start stocd

# Check logs
sudo journalctl -u stocd -f
```

> **Note**: Syncing from genesis will take a long time depending on your hardware and network speed. The node needs to process all blocks from block 1.

---

## Scenario 3: Become a Validator

### Prerequisites

- A fully synced node (Scenario 1 or 2 completed)
- STOC tokens for self-delegation (minimum 1 STOC = 1000000ustoc)
- Secure backup of your validator keys

### Wait for Full Sync

```bash
# Check sync status — must show "false" before creating validator
stocd status | jq '.sync_info.catching_up'
```

### Create Wallet (or Import Existing)

```bash
# Create new wallet
stocd keys add <key_name>

# Or import existing wallet
stocd keys add <key_name> --recover
```

> **IMPORTANT**: Save your mnemonic phrase securely. Losing it means losing access to your funds.

### Fund Your Wallet

Transfer STOC tokens to your wallet address:

```bash
# Check your address
stocd keys show <key_name> -a

# Check balance
stocd query bank balances $(stocd keys show <key_name> -a)
```

### Create Validator

```bash
stocd tx staking create-validator \
  --amount=1000000ustoc \
  --pubkey=$(stocd tendermint show-validator) \
  --moniker="<your_validator_name>" \
  --chain-id=stoc \
  --commission-rate="0.05" \
  --commission-max-rate="0.20" \
  --commission-max-change-rate="0.01" \
  --min-self-delegation="1" \
  --gas="auto" \
  --gas-adjustment=1.5 \
  --gas-prices="0.001ustoc" \
  --from=<key_name>
```

### Verify Validator

```bash
# Check validator status
stocd query staking validator $(stocd keys show <key_name> --bech val -a)

# Check if your validator is in the active set
stocd query staking validators --limit 100 | grep <your_validator_name>
```

---

## Monitoring

### Check Node Status

```bash
# Sync status
stocd status | jq '.sync_info'

# Current block height
stocd status | jq '.sync_info.latest_block_height'

# Connected peers
stocd status | jq '.node_info'

# Peer count
curl -s http://127.0.0.1:26657/net_info | jq '.result.n_peers'
```

### Useful Commands

```bash
# Check account balance
stocd query bank balances <address>

# List all validators
stocd query staking validators --limit 100

# Check validator status (for validators)
stocd query slashing signing-info $(stocd tendermint show-validator)

# Unjail validator (if jailed)
stocd tx slashing unjail --from <key_name> --chain-id stoc --gas auto --gas-prices 0.001ustoc
```

### Log Management

```bash
# View recent logs
sudo journalctl -u stocd -n 100

# Follow logs in real-time
sudo journalctl -u stocd -f

# Check for errors
sudo journalctl -u stocd | grep -i error
```

---

## Firewall Configuration

```bash
# Required ports
sudo ufw allow 22/tcp     # SSH
sudo ufw allow 26656/tcp  # P2P (required for all nodes)

# Optional — only if you want to expose RPC/API publicly
# sudo ufw allow 26657/tcp  # Cosmos RPC
# sudo ufw allow 1317/tcp   # REST API
# sudo ufw allow 9090/tcp   # gRPC
# sudo ufw allow 8545/tcp   # EVM JSON-RPC
# sudo ufw allow 8546/tcp   # EVM WebSocket

sudo ufw enable
```

---

## EVM Support

STOC ships with Cosmos EVM v0.6.0. Node operators can enable Ethereum JSON-RPC endpoints by editing `~/.stoc/config/app.toml`:

```toml
[json-rpc]
enable = true
address = "0.0.0.0:8545"
ws-address = "0.0.0.0:8546"
api = "eth,net,web3,txpool,debug"
```

Restart the node (`sudo systemctl restart stocd`) to apply.

### EVM Endpoints

| Endpoint | Default Port |
|----------|--------------|
| JSON-RPC | `8545` |
| WebSocket | `8546` |

### MetaMask Configuration

| Field | Value |
|-------|-------|
| Network Name | STOC Mainnet |
| RPC URL | Your node JSON-RPC (`http://<host>:8545`) or public endpoint |
| Chain ID | See project announcements for the canonical EVM chain ID |
| Currency Symbol | STOC |
| Decimals | 18 |

> **Note**: Custom tokens created via the `x/stoc` module are Cosmos-only and are **not** accessible from the EVM. Only the native `ustoc`/`astoc` pair is bridged.

---

## Backup Important Files

Always back up these files before any maintenance:

```bash
# Validator key (CRITICAL for validators)
~/.stoc/config/priv_validator_key.json

# Node key
~/.stoc/config/node_key.json

# Configuration
~/.stoc/config/config.toml
~/.stoc/config/app.toml
```

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Peer connection issues | Re-fetch peers from addrbook API |
| Genesis mismatch | Re-download genesis file |
| Disk space full | Check `du -sh ~/.stoc/data` and consider pruning |
| Node not syncing | Check firewall (port 26656), check peers, check logs |
| Validator jailed | Wait for jail period, then run unjail command |
| Out of memory | Increase RAM or add swap space |

---

## Support

- **GitHub Issues**: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet/issues
- **Block Explorer**: https://stochainscan.io
