# Binary history — STOChain mainnet (chain-id `stoc`)

Which commit produced which range of blocks, from genesis to the current head.

Mainnet has run **seven** binaries since it launched on 2025-04-24. Only **three** of those changes
went through governance; the other four were swapped in without a proposal, so nothing on chain records
them. Anyone replaying the chain from block 1 needs all seven, in order, or their app hash diverges and
the node stops.

## Version scheme

A whole number is a governance upgrade and matches the plan name recorded on chain. A decimal marks a
binary swapped **without** a proposal — those boundaries were found by re-executing the chain and
comparing app hashes, not from any on-chain record.

**v4 does not exist on mainnet.** The v4 through v8 iterations ran on devnet only and were consolidated
into the single v5.0.0 release.

## The map

| Version | From block | To block | Commit | Go toolchain | Governance | Status |
|---|---|---|---|---|---|---|
| `v1` | 1 | 542,404 | [`aa5f67b`](../../commit/aa5f67b) | `go1.24.3` | — | replay-verified |
| `v1.1` | 542,405 | 2,709,241 | [`50f6982`](../../commit/50f6982) | `go1.24.3` | — | replay-verified |
| `v1.2` | 2,709,242 | 4,455,466 | [`a2d23f3`](../../commit/a2d23f3) | `go1.24.3` | — | replay-verified |
| `v2-evm` | 4,455,467 | 4,705,315 | *being identified* | `go1.24.3` | ✔ prop #2 | in progress |
| `v3` | 4,705,316 | ≈ 4,706,501 | [`4d47b49`](../../commit/4d47b49) | `go1.24.3` | ✔ prop #4 | not yet replayed |
| `v3.1` | ≈ 4,794,077 | 6,408,099 | [`2f8e6c1`](../../commit/2f8e6c1) | `go1.24.3` | — | not yet replayed |
| `v5.0.0` | 6,408,100 | head | [`fe53cbd`](../../commit/fe53cbd) | `go1.25.8` | ✔ prop #5 | current |

Tags exist for the rows marked *replay-verified* and for `v5.0.0`. A row is only tagged once a node has
re-executed that range from block 1 and matched mainnet — pinning a tag we had not proven is exactly the
mistake that broke partner sync in July 2026.

### What each version changes

- **v1** — genesis binary. Cosmos only, no EVM, no upgrade handlers. cosmos-sdk v0.50.11, CometBFT v0.38.12.
- **v1.1** — cosmos-sdk v0.50.11 → v0.53.0, CometBFT 0.38.12 → 0.38.17, ibc-go 8.5.1 → 8.7.0. Empty blocks
  execute identically on v1 and v1.1, so the difference only appears at the first transaction after the
  switch: a `MsgSend` in block 542,405.
- **v1.2** — adds `MsgBurnToken` to `x/stoc`. Earlier binaries have no handler for it, and the first burn
  on mainnet is in block 2,709,242.
- **v2-evm** — governance upgrade. EVM introduced (cosmos/evm v1.0.0-rc2). The upgrade handler itself
  writes state, so the binary has to match exactly; we are still identifying which commit mainnet ran.
- **v3** — governance upgrade `v3-fix-evm-denom`. cosmos/evm v0.6.0, EVM denom corrected.
- **v3.1** — the 100 STOC token-creation fee is removed. The `MsgCreateToken` at 4,706,501 still burns it;
  the one at 4,794,077 does not. No token creation happens in between, so any switch height inside that
  window produces identical state.
- **v5.0.0** — governance upgrade. Current consensus.

## Two things that will cost you a day if you miss them

**Set the EVM chain ID before you start.** The cosmos chain-id is plain `stoc` with no EVM number in it,
so any post-`v2-evm` binary aborts with `cannot derive EVM chain ID from cosmos chain-id "stoc"`. A node
initialised with the genesis binary has no `[evm]` section at all, so this only surfaces millions of
blocks in. Put it in `app.toml` up front:

```toml
[evm]
evm-chain-id = 1306
```

**Copy the data directory before every governance boundary** — 4,455,466 · 4,705,315 · 6,408,099.

```bash
systemctl stop <your node>
cp -a ~/.stoc/data ~/stoc-checkpoint-$(date +%s)   # cp, not tar/zip
systemctl start <your node>
```

Elsewhere a wrong binary is recoverable: `stocd rollback` rewinds one block and the next binary
re-executes it. A governance upgrade also *adds stores* — `v2-evm` creates `vm`, `feemarket`, `erc20`
and `evmutil`. If the new layout commits and the resulting app hash then fails validation, the store
metadata records height H while the older stores hold only H−1, and `start`, `rollback` and even
`rollback --hard` all abort with `wanted to load target H but only found up to H−1` while the app is
still being constructed. Nothing that needs the app can run. The only ways out are a copy taken
beforehand or a full re-sync from block 1.

## Building

`GOTOOLCHAIN=go1.24.3` builds every commit except `v5.0.0`, whose `go.mod` requires go ≥ 1.25.8. Do not
simply use the newest Go for all of them — `50f6982` fails to compile on Go 1.25, because
`bytedance/sonic` v1.13.2 reaches into runtime internals that release removed.

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

for tag in v1 v1.1 v1.2; do
  git checkout $tag
  CGO_ENABLED=1 GOTOOLCHAIN=go1.24.3 go build -o ~/stocd-bins/stocd_$tag ./cmd/stocd
done

git checkout v5.0.0
CGO_ENABLED=1 GOTOOLCHAIN=go1.25.8 go build -o ~/stocd-bins/stocd_v5.0.0 ./cmd/stocd
```

A plain clone contains every commit listed here, including the ones with no branch of their own —
they are ancestors of `release/v5.0.0`. Fetching a bare SHA without cloning first does not work.

## Verifying your replay

Compare your node's app hash against the live chain at each boundary. A match means every block below
it replayed identically, which confines any fault to one range instead of the whole chain.

```bash
curl -s https://rpc-stoc-mainnet.stochainscan.io/block?height=4455467 \
  | jq -r .result.block.header.app_hash
curl -s http://localhost:26657/block?height=4455467 \
  | jq -r .result.block.header.app_hash
```

| Height | Expected app hash |
|---|---|
| 4,455,466 | `A64CF4F7184F7A89C400AA73E87E0F9F52EB179370A99E6C6B36F298120C10B5` |
| 4,455,467 | `3DC21C2A02285A4C465BA306D63F7273FFFB020F7BBCF4F1676E9B431D30DA41` |
| 4,705,315 | `B6032442B9FE68CCE8DC5694265F81C548E9C75846ED76B0A54E7284D29D511C` |
| 4,705,316 | `C64073F64114BB7C8F60AB47AA5DE5BD258FA2697003DDBB6427DA083C5CF698` |
| 6,408,099 | `9B1D4E0032A49C414122893FD0B4100ED9EE3DB07E3109974DA2607888647D01` |
| 6,408,100 | `AFD78BE6C4B343CD336EDE6FD4D3EEBC997B65BD4F9E3BEB63AB8B67FBFEC457` |

## If you only need a current node

Re-executing history is only necessary if you want to verify it yourself. For a working node, the daily
snapshot restores in under an hour and needs none of the above:

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet && git checkout v5.0.0 && make install

stocd init <moniker> --chain-id stoc
curl -s https://api-stoc-mainnet.stochainscan.io/rpc/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json
curl -L -o snapshot.tar.gz https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
tar -xzf snapshot.tar.gz -C ~/.stoc
```
