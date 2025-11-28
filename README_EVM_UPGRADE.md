# STOC Chain - EVM Upgrade Implementation

## 🎯 Quick Start

### Files Generated ✅
1. ✅ `app/evm.go` - EVM module setup (6 decimals)
2. ✅ `app/ante/ante.go` - Ante handler router
3. ✅ `app/ante/evm_handler.go` - EVM ante handler
4. ✅ `app/upgrades.go` - Upgrade handler for v2-evm
5. ✅ `go.mod` - Updated with IBC v10 + EVM dependencies

### Next Steps Required

#### 1. Complete Manual File Updates
Follow detailed instructions in:
**[IMPLEMENTATION_STEPS.md](./IMPLEMENTATION_STEPS.md)**

Files that need manual updates:
- [ ] `app/app.go` - Add EVM keepers and setup
- [ ] `app/app_config.go` - Add EVM modules to config
- [ ] `cmd/stocxxxd/cmd/root.go` - Add EVM CLI commands

#### 2. Build and Test
```bash
# Install dependencies
go mod tidy

# Build
make build
# or
make install

# Test build
./build/stocxxxd version
```

#### 3. Test Locally
```bash
# Initialize local node
stocxxxd init test --chain-id=stoc-local

# Add test key
stocxxxd keys add validator --keyring-backend=test

# Add genesis account
stocxxxd genesis add-genesis-account validator 1000000000000ustoc --keyring-backend=test

# Create genesis tx
stocxxxd genesis gentx validator 1000000ustoc --chain-id=stoc-local --keyring-backend=test

# Collect gentx
stocxxxd genesis collect-gentxs

# Start chain
stocxxxd start
```

#### 4. Test EVM
```bash
# Test JSON-RPC
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:8545

# Should return EVM chain ID
```

---

## 📋 Implementation Checklist

### Code Changes
- [ ] Review `app/evm.go` (decimals = 6 ✓)
- [ ] Review `app/ante/*.go`
- [ ] Review `app/upgrades.go`
- [ ] Update `app/app.go` (see IMPLEMENTATION_STEPS.md)
- [ ] Update `app/app_config.go` (see IMPLEMENTATION_STEPS.md)
- [ ] Update `cmd/root.go` (see IMPLEMENTATION_STEPS.md)
- [ ] Run `go mod tidy`
- [ ] Fix all import errors

### Build & Test
- [ ] `make build` successful
- [ ] Local node starts
- [ ] JSON-RPC responds (port 8545/8546)
- [ ] Can send Cosmos tx
- [ ] Can deploy smart contract
- [ ] Upgrade process tested

### Testnet
- [ ] Deploy to testnet
- [ ] Create upgrade proposal
- [ ] Test upgrade at specific height
- [ ] Verify all functionality
- [ ] Monitor metrics

### Mainnet
- [ ] Final testnet validation
- [ ] Create mainnet upgrade proposal
- [ ] Governance vote
- [ ] Coordinate with validators
- [ ] Execute upgrade
- [ ] Monitor closely

---

## ⚠️ Critical Configuration

### Decimals = 6 (Not 18!)
```go
// app/evm.go:58
Decimals: evmtypes.SixDecimals,
```

**Why 6 decimals:**
- ✅ No state migration needed
- ✅ Preserves existing balances
- ✅ Compatible with Cosmos ecosystem
- ⚠️ MetaMask will display wrong (need proxy)

**See:** [ai-summary/EVM_CONFIG_CHANGES.md](../ai-summary/2025-11-28-15-45/EVM_CONFIG_CHANGES.md)

### Upgrade Name
```go
// app/upgrades.go:14
const UpgradeName = "v2-evm"
```

**Use this name in upgrade proposal!**

---

## 🚀 Upgrade Process (For Running Chain)

### 1. Prepare Upgrade Proposal
```bash
stocxxxd tx gov submit-proposal software-upgrade v2-evm \
  --title="Add EVM Support to STOC Chain" \
  --description="Upgrade to add EVM support with 6 decimals, maintaining all existing functionality" \
  --upgrade-height=<FUTURE_HEIGHT> \
  --upgrade-info='{}' \
  --deposit=10000000ustoc \
  --from=validator \
  --chain-id=<CHAIN_ID>
```

### 2. Vote on Proposal
```bash
# Vote yes
stocxxxd tx gov vote <PROPOSAL_ID> yes --from=validator
```

### 3. Prepare New Binary
```bash
# Build new binary with EVM
make install

# Verify version
stocxxxd version
```

### 4. Distribute to Validators
- Send new binary to all validators
- Test on testnet first!
- Setup Cosmovisor (optional but recommended)

### 5. At Upgrade Height
- Chain automatically halts
- Upgrade handler runs:
  - Adds EVM store keys
  - Initializes EVM modules
  - No state reset!
- Chain resumes with EVM support

### 6. Verify After Upgrade
```bash
# Check upgrade applied
stocxxxd q upgrade applied v2-evm

# Test JSON-RPC
curl http://localhost:8545 \
  -X POST \
  -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'
```

---

## 📁 File Structure

```
stoc/
├── app/
│   ├── evm.go                 ✅ NEW - EVM module setup
│   ├── upgrades.go            ✅ NEW - Upgrade handler
│   ├── app.go                 ⏳ UPDATE - Add EVM keepers
│   ├── app_config.go          ⏳ UPDATE - Module config
│   └── ante/
│       ├── ante.go            ✅ NEW - Ante router
│       └── evm_handler.go     ✅ NEW - EVM ante
├── cmd/stocxxxd/cmd/
│   └── root.go                ⏳ UPDATE - Add EVM commands
├── go.mod                     ✅ UPDATED - IBC v10 + EVM
├── IMPLEMENTATION_STEPS.md    ✅ NEW - Detailed guide
└── README_EVM_UPGRADE.md      ✅ THIS FILE
```

---

## 📚 Documentation

### In this directory:
- **[IMPLEMENTATION_STEPS.md](./IMPLEMENTATION_STEPS.md)** - Complete step-by-step guide
- **[app/evm.go](./app/evm.go)** - EVM setup (read comments)
- **[app/upgrades.go](./app/upgrades.go)** - Upgrade logic

### In ai-summary folder:
- **[MIGRATION_PLAN.md](../ai-summary/2025-11-28-15-45/MIGRATION_PLAN.md)** - Overall plan
- **[EVM_CONFIG_CHANGES.md](../ai-summary/2025-11-28-15-45/EVM_CONFIG_CHANGES.md)** - Config details
- **[IMPLEMENTATION_SUMMARY.md](../ai-summary/2025-11-28-15-45/IMPLEMENTATION_SUMMARY.md)** - Summary
- **[SUMMARY.md](../ai-summary/2025-11-28-15-45/SUMMARY.md)** - Executive summary

---

## 🔍 Troubleshooting

### Build Errors

**Import errors:**
```bash
go mod tidy
```

**Missing dependencies:**
```bash
go get github.com/cosmos/evm@latest
go get github.com/cosmos/ibc-go/v10@latest
```

### Runtime Errors

**Store not found:**
- Check `app/upgrades.go` has correct store keys
- Verify upgrade handler registered in `app/app.go`

**Ante handler routing:**
- Check `app/ante/ante.go` imports correct
- Verify `ante.IsEVMTransaction(tx)` logic

**JSON-RPC not responding:**
- Check `app.toml` has `[json-rpc]` section
- Verify `setEVMMempool()` called in `app/app.go`
- Check ports 8545/8546 not blocked

---

## 🎯 Key Features After Upgrade

### Cosmos Features (Existing)
- ✅ All existing Cosmos transactions
- ✅ IBC transfers (upgraded to v10)
- ✅ Staking, governance, slashing
- ✅ Bank transfers
- ✅ All custom modules

### NEW: EVM Features
- ✅ Deploy Solidity smart contracts
- ✅ Call contract functions
- ✅ Ethereum transactions (via JSON-RPC)
- ✅ MetaMask support (with RPC proxy)
- ✅ Web3.js / ethers.js compatible
- ✅ ERC20 ↔ Cosmos coin conversion

### Hybrid Features
- ✅ Unified mempool (Cosmos + EVM)
- ✅ Single fee token
- ✅ Shared security
- ✅ Cross-ecosystem interop

---

## ⚙️ Configuration Files

### app.toml (after deployment)
```toml
[evm]
tracer = ""

[json-rpc]
enable = true
address = "0.0.0.0:8545"
ws-address = "0.0.0.0:8546"
allow-unprotected-txs = false
enable-unsafe = false
api = ["eth", "net", "web3", "txpool"]
```

### Genesis (auto-added by upgrade)
- `evm` module state
- `feemarket` module state
- `erc20` module state

---

## 📊 Monitoring

### Metrics to Watch
- Block time
- Transaction throughput
- JSON-RPC response time
- Gas usage
- Mempool size
- EVM transaction count
- Error rate

### Logs to Monitor
```bash
# Watch logs
tail -f ~/.stoc/logs/stoc.log | grep -E "(evm|upgrade|ante)"
```

---

## 🆘 Emergency Procedures

### If Upgrade Fails
1. Chain will halt at upgrade height
2. Check logs for errors
3. Fix code issue
4. Rebuild binary
5. Restart with fixed binary

### Rollback (if needed)
1. Stop chain
2. Revert to old binary
3. Use `--unsafe-skip-upgrades` flag
4. Investigate issue
5. Fix and retry

**Always test on testnet first!**

---

## ✨ Success Indicators

After upgrade, verify:
- [ ] Chain producing blocks
- [ ] All validators online
- [ ] Cosmos tx work
- [ ] EVM tx work
- [ ] JSON-RPC responds
- [ ] Can deploy contracts
- [ ] IBC still functional
- [ ] No balance discrepancies

---

## 📞 Support

**Issues?** Check:
1. [IMPLEMENTATION_STEPS.md](./IMPLEMENTATION_STEPS.md) first
2. Generated code comments
3. Compare with `gm` reference implementation
4. Cosmos EVM docs: https://github.com/cosmos/evm

**Questions?**
- Review documentation in `ai-summary/` folder
- Check IBC v10 migration guide
- Test on local node to reproduce

---

## 🎓 Important Notes

1. **Decimals = 6**: Chain uses 6 decimals, not 18
2. **No Reset**: Upgrade doesn't reset state
3. **IBC v10**: Breaking changes from v8
4. **Test First**: Always test on testnet!
5. **Backup**: Keep backups before upgrade
6. **Coordinate**: Work with validators

---

**Ready to start?**

1. Read [IMPLEMENTATION_STEPS.md](./IMPLEMENTATION_STEPS.md)
2. Complete manual file updates
3. Run `go mod tidy`
4. Test build
5. Test locally
6. Deploy to testnet

**Good luck! 🚀**
