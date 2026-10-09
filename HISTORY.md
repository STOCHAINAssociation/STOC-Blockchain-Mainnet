# Binary history — STOChain mainnet (chain-id `stoc`)

Which source produced which range of blocks, from genesis to the current head.

Mainnet has run **seven** binaries since it launched on 2025-04-24. Only **three** of those changes
went through governance; the other four were swapped in without a proposal, so nothing on chain records
them. Anyone replaying the chain from block 1 needs all of them, in order, or their app hash diverges and
the node stops.

Replaying takes **eight** steps rather than seven, because no single source reproduces the `v2-evm`
range on its own. See the `v2-evm` note below — two sources in sequence reproduce the same state.

## Version scheme

A whole number is a governance upgrade and matches the plan name recorded on chain. A decimal marks a
binary swapped **without** a proposal — those boundaries were found by re-executing the chain and
comparing app hashes, not from any on-chain record.

**v4 does not exist on mainnet.** The release after `v3.1` is `v5.0.0`.

## The map

| Version | From block | To block | Source | Go toolchain | Governance | Status |
|---|---|---|---|---|---|---|
| `v1` | 1 | 542,404 | [`v1`](../../tree/phase/v1) | `go1.24.3` | — | replay-verified |
| `v1.1` | 542,405 | 2,709,241 | [`v1.1`](../../tree/phase/v1.1) | `go1.24.3` | — | replay-verified |
| `v1.2` | 2,709,242 | 4,455,466 | [`v1.2`](../../tree/phase/v1.2) | `go1.24.3` | — | replay-verified |
| `v2-evm` *(upgrade block only)* | 4,455,467 | 4,455,467 | [`v2-evm`](../../tree/phase/v2-evm) | `go1.24.3` | ✔ prop #2 | replay-verified |
| `v2-evm` *(remainder)* | 4,455,468 | 4,699,537 | [`v2-evm`](../../tree/phase/v2-evm) | `go1.24.3` | — | replay-verified |
| `v2-evm` *(tail)* | 4,699,538 | 4,705,315 | [`v2-evm-tail`](../../tree/phase/v2-evm-tail) | `go1.24.3` | — | replay-verified |
| `v3` | 4,705,316 | 4,794,076 | [`v3`](../../tree/phase/v3) | `go1.24.3` | ✔ prop #4 | replay-verified |
| `v3.1` | 4,794,077 | 6,408,099 | [`v3.1`](../../tree/phase/v3.1) | `go1.24.3` | — | replay-verified |
| `v5.0.0` | 6,408,100 | head | [`v5.0.0`](../../tree/phase/v5.0.0) | `go1.25.8` | ✔ prop #5 | replay-verified |

`v5.0.1` (tag `v5.0.1`, the `main` branch) deliberately has no row. It is app-hash-identical to
`v5.0.0` and owns no replay range — a `v5.0.0` binary re-executes the same blocks to the same hashes.
Run `v5.0.1` anyway: it carries a security fix. See *What each version changes* below.

Every source in the table is a tag with a matching branch, `phase/<tag>`; `main` is `v5.0.1`. A source is only tagged
once a node has re-executed its range from block 1 and matched mainnet.

Every boundary above was established by re-executing the block and comparing the resulting app hash
against mainnet. Every height listed is exact.

A node has now replayed the whole chain against this table — block 1 to the live head, every boundary
crossed in the order given, no app-hash divergence anywhere. The sequence below is known to work, not
inferred.

### What each version changes

- **v1** — genesis binary. Cosmos only, no EVM, no upgrade handlers. cosmos-sdk v0.50.11, CometBFT v0.38.12.
- **v1.1** — cosmos-sdk v0.50.11 → v0.53.0, CometBFT 0.38.12 → 0.38.17, ibc-go 8.5.1 → 8.7.0. Empty blocks
  execute identically on v1 and v1.1, so the difference only appears at the first transaction after the
  switch: a `MsgSend` in block 542,405.
- **v1.2** — adds `MsgBurnToken` to `x/stoc`. Earlier binaries have no handler for it, and the first burn
  on mainnet is in block 2,709,242, which is the entire boundary: that block holds one transaction and it
  is a `MsgBurnToken`.
- **v2-evm** — governance upgrade. EVM introduced (cosmos/evm v1.0.0-rc2), adding the `vm`, `feemarket`,
  `erc20` and `evmutil` stores. **This range needs two sources**, for a reason worth understanding before
  you start:
  - `v2-evm` reproduces the upgrade block itself. The upgrade handler writes state, so only a matching
    source yields mainnet's app hash. `v2-evm-tail` — the last source written before the trigger, and the obvious
    guess — executes the upgrade cleanly but produces a different hash.
  - `v2-evm` then carries the range as far as **4,699,537**. It cannot go further: it also contains the
    `v3-fix-evm-denom` handler, so the moment proposal #4 writes the v3 plan into state at block 4,699,538,
    x/upgrade aborts with `BINARY UPDATED BEFORE TRIGGER`. `v2-evm-tail` — which predates that handler — takes
    the remaining 5,777 blocks to 4,705,315.
  - Do **not** use `v2-evm-tail` for the whole span. The two sources differ only in `app/upgrades.go`, so it is
    tempting to assume either will do, and that assumption is wrong for a reason unrelated to consensus:
    `v2-evm-tail` reliably dies a few hundred blocks in, on the nil base fee described under *Three things*
    below. Both orders were tried; only this one runs to the end.
  - Mainnet itself ran a single binary here, equivalent to `v2-evm` without the v3 handler. The
    two-source sequence reproduces the same state, which is what a replay needs.
- **v3** — governance upgrade `v3-fix-evm-denom`. cosmos/evm v0.6.0, EVM denom corrected. This binary
  carries the chain a further 88,760 blocks, to 4,794,076.
- **v3.1** — the 100 STOC token-creation fee is removed. The boundary is exactly **4,794,077**: that block
  holds a single `MsgCreateToken`, dated 2026-04-14. Executed with `v3` the fee is still charged and
  the app hash is wrong; executed with `v3.1` it matches mainnet. No token creation happens between
  4,706,501 and 4,794,077, so any switch height inside that window produces identical state — but 4,794,077
  is the first block where the choice becomes visible, and the last one where it is still free.
- **v5.0.0** — governance upgrade. Current consensus. Unlike `v2-evm` this is a param-only migration
  (EIP-1559 feemarket enable) with **no store migration**, so a wrong binary here is recoverable with
  `stocd rollback` like any ordinary mismatch.
- **v5.0.1** — security + stability patch, **app-hash-identical to v5.0.0**. Deployed 2026-08-28 via a
  zero-downtime rolling binary swap; no governance proposal — it changes no consensus behaviour, so it
  owns no distinct replay range (a v5.0.0 node syncs the same blocks). Two fixes: (1) backport of the
  upstream cosmos/evm **v0.6.3** security patch — **GHSA-367m-g444-9mg3 "non-atomic StateDB commit"**
  (`StateDB.Commit()` atomicity + locked-balance snapshot + under/overflow panics in `x/vm/statedb`);
  (2) nil base-fee guard in the London header (`CalcBaseFee` nil-deref, described above). Build it from
  tag `v5.0.1` (the `main` branch). Do not compare the binary's SHA256
  with anyone else's: the hash also depends on the build directory and the embedded commit, so a correct
  build on another machine produces a different hash. Verify it the same way as any self-built binary —
  the node keeps syncing with matching app hashes. Operators should run v5.0.1 for the security fix,
  though it is not required to stay in sync.

## Three things that will cost you a day if you miss them

**Set the EVM chain ID before you start.** The cosmos chain-id is plain `stoc` with no EVM number in it,
so any post-`v2-evm` binary aborts with `cannot derive EVM chain ID from cosmos chain-id "stoc"`. A node
initialised with the genesis binary has no `[evm]` section at all, so this only surfaces millions of
blocks in. Put it in `app.toml` up front:

```toml
[evm]
evm-chain-id = 1306
```

**Stop cleanly before a boundary, with `halt-height`.** Watching the height and stopping by hand does not
work — at 20 blocks per second a five-second poll overshoots by a hundred blocks. Set `halt-height` in the
`[base]` section of `app.toml` and restart; the node refuses the target block before executing it and
exits, leaving the store clean at the previous height.

```toml
halt-height = 4455000   # stop with margin, take your copy, then reset to 0
```

Leave it set and the node will commit one block and halt again, forever — reset it to `0` before starting
the next binary.

**Let the old binary reach the trigger.** This is the trap that `halt-height` creates. A governance
upgrade that adds stores only works if `data/upgrade-info.json` exists, and x/upgrade writes that file at
the moment the *old* binary reaches the trigger height and panics `UPGRADE NEEDED` — inside BeginBlocker,
before anything is committed. Halt the old binary *at* the trigger and the file is never written, so every
subsequent binary refuses to boot:

```
panic: failed to load latest version: version of store evm mismatch root store's version;
       expected 4455466 got 0; new stores should be added using StoreUpgrades
```

The order that works, at every governance boundary:

```bash
# 1. stop with margin and take a copy
halt-height = 4455000   → copy ~/.stoc/data somewhere safe

# 2. old binary, halt-height = 0 → it reaches 4,455,467, writes upgrade-info.json,
#    panics UPGRADE NEEDED and exits. Nothing is committed.

# 3. new binary, halt-height = <trigger + 1> → executes exactly the upgrade block
```

`halt-height` exits with code **2**; the `UPGRADE NEEDED` panic exits with **1**. Neither is a crash.

## Two crashes that are not your fault

**`set min gas price in app.toml`** — the genesis binary refuses to start until
`minimum-gas-prices` has a value; a fresh `stocd init` leaves it empty. Mainnet uses `0.001ustoc`. This is
a node-local mempool policy and does not affect consensus, so any value lets the replay proceed, but the
node will not boot without one.

**A SIGSEGV in the EVM mempool, on binaries of the `v2-evm` era.** It looks fatal and is not:

```
panic: runtime error: invalid memory address or nil pointer dereference
  eip1559.CalcBaseFee
  legacypool.(*LegacyPool).runReorg
```

Peers gossip EVM transactions into the legacy pool, the pool's reorg loop reads a London header whose base
fee is nil, and the process dies. It happens in a mempool goroutine, never mid-commit — Cosmos commits a
block atomically, so the committed state is intact and restarting resumes from the next block. Because the
trigger is whatever the network happens to gossip, it is not reproducible at a fixed height and it can hit
any binary of that era.

Restart and carry on; expect to do it more than once across the EVM range. `v5.0.1` fixes it
(*guard nil base fee in London header to prevent CalcBaseFee nil-deref*), so the range from 6,408,100
onward is unaffected.

## Copy the data directory before every governance boundary

Boundaries: 4,455,466 · 4,705,315 · 6,408,099.

```bash
systemctl stop <your node>
cp -a ~/.stoc/data ~/stoc-checkpoint-$(date +%s)   # cp, not tar/zip
systemctl start <your node>
```

Elsewhere a wrong binary is recoverable: `stocd rollback` rewinds one block and the next binary
re-executes it — that is how the 4,794,077 boundary above is crossed, with no copy needed. A governance
upgrade also *adds stores*. If the new layout commits and the resulting app hash then fails validation,
the store metadata records height H while the older stores hold only H−1, and `start`, `rollback` and even
`rollback --hard` all abort with `wanted to load target H but only found up to H−1` while the app is
still being constructed. Nothing that needs the app can run. The only ways out are a copy taken
beforehand or a full re-sync from block 1.

Note that `halt-height` does not protect you *after* a bad upgrade: CometBFT rejects the following block
during header validation, before `FinalizeBlock`, so the halt check never runs and the node retries in a
loop instead of stopping. The wrong hash is already committed by then.

## Building

`GOTOOLCHAIN=go1.24.3` builds every tag except `v5.0.0` and `v5.0.1`, whose `go.mod` requires go ≥ 1.25.8. Do not
simply use the newest Go for all of them — `v1.1` fails to compile on Go 1.25, because
`bytedance/sonic` v1.13.2 reaches into runtime internals that release removed.

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet

for tag in v1 v1.1 v1.2 v2-evm v2-evm-tail v3 v3.1; do
  git checkout $tag
  CGO_ENABLED=1 GOTOOLCHAIN=go1.24.3 go build -o ~/stocd-bins/stocd_$tag ./cmd/stocd
done

git checkout v5.0.0
CGO_ENABLED=1 GOTOOLCHAIN=go1.25.8 go build -o ~/stocd-bins/stocd_v5.0.0 ./cmd/stocd

# The binary to actually run. Same app hashes as v5.0.0, so it replays the
# v5.0.0 range too — build it instead if you are not testing that boundary.
git checkout v5.0.1
CGO_ENABLED=1 GOTOOLCHAIN=go1.25.8 go build -o ~/stocd-bins/stocd_v5.0.1 ./cmd/stocd
```

Each tag also has a branch of the same name under `phase/`, for example `git checkout phase/v3`.

These builds carry no `ldflags`, so `stocd version` prints an empty string. To confirm which source a
binary came from, read the VCS stamp Go embeds automatically:

```bash
go version -m ~/stocd-bins/stocd_v2-evm | grep vcs.revision
```

## Verifying your replay

Compare your node's app hash against the live chain at each boundary. A match means every block below
it replayed identically, which confines any fault to one range instead of the whole chain.

```bash
curl -s https://rpc-stoc-mainnet.stochainscan.io/block?height=4455467 \
  | jq -r .result.block.header.app_hash
curl -s http://localhost:26657/block?height=4455467 \
  | jq -r .result.block.header.app_hash
```

A block header carries the app hash produced by the block *before* it, so the row for height H below is
what your node must hold after executing H−1.

| Height | Expected app hash |
|---|---|
| 4,455,466 | `A64CF4F7184F7A89C400AA73E87E0F9F52EB179370A99E6C6B36F298120C10B5` |
| 4,455,467 | `3DC21C2A02285A4C465BA306D63F7273FFFB020F7BBCF4F1676E9B431D30DA41` |
| 4,455,468 | `6ADDC6A300E138ECF2886B6B81013879F6EDF74FBD2BA64F34CD7045A47E9A3E` |
| 4,699,538 | `AA848D7C8AF62F4FF6C1196A4B9D5FC399B1793A4227BACAE7DB3A176E8CF12D` |
| 4,705,315 | `B6032442B9FE68CCE8DC5694265F81C548E9C75846ED76B0A54E7284D29D511C` |
| 4,705,316 | `C64073F64114BB7C8F60AB47AA5DE5BD258FA2697003DDBB6427DA083C5CF698` |
| 4,794,077 | `9E99E79A05ADF8596D9B252C8480129C87D54C1E1D263570F9E7AC7DA0E583A0` |
| 4,794,078 | `068E74A54D264C6E6879610AD8E349211D88AF44D01C45393D03C73ECAD880D2` |
| 6,408,099 | `9B1D4E0032A49C414122893FD0B4100ED9EE3DB07E3109974DA2607888647D01` |
| 6,408,100 | `AFD78BE6C4B343CD336EDE6FD4D3EEBC997B65BD4F9E3BEB63AB8B67FBFEC457` |
| 6,408,101 | `C9701C0F809C5986DE8D3333BA8DF75BCE154885DACFBD49FE99A22488FEB370` |

The 4,455,468 and 4,794,078 rows are the two that catch a wrong binary choice earliest: the first
distinguishes `v2-evm` from `v2-evm-tail` at the EVM upgrade, the second distinguishes `v3.1` from
`v3` at the token-fee change.

## The chain is its own record

For a governance upgrade you do not have to trust this file. The plan's `info` field is written on chain
by the proposal and carries the deployment details, including the binary hash. Your
node writes it to `data/upgrade-info.json` the moment the old binary halts at the trigger:

```json
{"name":"v5.0.0","height":6408100,"info":"… Linux amd64 binary SHA256 c4185ed3069650f6f4e67e138a64095b26f892143ef115b530bc253a07fcf6af. …"}
```

The source for this release is tag `v5.0.0` (branch `phase/v5.0.0`) in this repository. The consensus source is the
only thing that determines the app hash, so any correct build of it produces a node that syncs. Verify a
binary you were given by its SHA256; verify a binary you built yourself by replaying a boundary and
comparing app hashes.

## If you only need a current node

Re-executing history is only necessary if you want to verify it yourself. For a working node, the daily
snapshot restores in under an hour and needs none of the above:

```bash
git clone https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet.git
cd STOC-Blockchain-Mainnet && git checkout v5.0.1 && make install

stocd init <moniker> --chain-id stoc
curl -s https://api-stoc-mainnet.stochainscan.io/rpc/genesis | jq '.result.genesis' > ~/.stoc/config/genesis.json
curl -L -o snapshot.tar.gz https://api-sync-stoc-mainnet.stochainscan.io/snapshots/download-latest
tar -xzf snapshot.tar.gz -C ~/.stoc
```
