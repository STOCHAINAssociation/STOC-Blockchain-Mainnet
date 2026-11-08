# Binary history — STOChain testnet (chain-id `tstoc_1999-1`, EVM chain id 1999)

Which binary produced which range of blocks on the **current** testnet chain, plus a log of past reset cycles.
Mainnet has its own file: [HISTORY.md](HISTORY.md).

Testnet is not permanent. Every two to three months it is **reset**: a fresh genesis built from the newest
code, new validator keys, and the history below starts again from block 1 (the faucet account is kept).
Upgrades in between go through governance, exactly like mainnet, so mainnet releases are rehearsed here first.
This file is updated at every upgrade and at every reset.

## Current cycle — genesis 2026-09-30

Genesis time `2026-09-30T10:27:47Z`, initial height 1. Four nodes: three validators plus one public full node
(RPC / REST / EVM JSON-RPC at `*-stoc-testnet.stochainscan.io`).

| Version | From block | To block | Source | Go | Governance | Binary SHA256 (linux/amd64) |
|---|---|---|---|---|---|---|
| `v5.0.1` | 1 (genesis) | rolling swap 2026-10-30 | tag [`v5.0.1`](../../tree/v5.0.1) | `go1.25.8` | — (genesis) | `0ab5a710536562ce31ba4bc3773aa81e0ee15dcc9f94efde9bf3100a1a26d723` |
| `v5.0.2` | rolling swap 2026-10-30 | 586,279 | not published — non-consensus, folded into `v5.0.3` | `go1.26.10` | — (rolling swap) | `336192802e166f458bdd7f1a13532a2328e9967c1d235a367c6a74001123f417` |
| `v5.0.3` | 586,280 | 607,279 | tag [`v5.0.3`](../../tree/v5.0.3) | `go1.26.10` | ✔ prop #2 | `d81a0a023d6ce0deb7d2f4581738b5693b3934ee5b3424b988b75d6db873a2f5` |
| `v6.0.0-rc3` | 607,280 | head | published with the `v6.0.0` release | `go1.26.10` | ✔ prop #3 | `3d7c4829a48a4f2329f3faef1cb2b5eba943e80bb9ed597003f00e4ff56b36f9` |

Hashes are of the binaries the testnet nodes actually run. A build of the same source on another machine can
have a different hash and still produce the same app hashes.

### Governance on this cycle

| Proposal | Passed | What |
|---|---|---|
| #1 | 2026-10-08 | feemarket params: base fee enabled (EIP-1559), parity with mainnet. No binary change. |
| #2 | 2026-11-06 | software upgrade `v5.0.3` at height 586,280 (block time 2026-11-06 04:37:23 UTC). |
| #3 | 2026-11-07 | software upgrade `v6.0.0` at height 607,280 (block time 2026-11-07 12:12:26 UTC). |

### What each version changes

- **v5.0.1** — genesis binary of this cycle. Same consensus code as mainnet `v5.0.1`.
- **v5.0.2** — EVM mempool and JSON-RPC fixes plus dependency security updates. No state-machine change, so it
  was swapped one node at a time with no proposal; mixed `v5.0.1` / `v5.0.2` nodes kept the same app hash.
  Pre-release test builds of it ran on testnet on 2026-10-29 and 2026-10-30 before the final build.
- **v5.0.3** — the security release, same source as mainnet `v5.0.3` (see [HISTORY.md](HISTORY.md)).
- **v6.0.0-rc3** — release candidate of `v6.0.0`. The native coin moves from 6 to 18 decimals in place:
  base denom `utstoc` → `atstoc`, every amount ×10¹², 1 TSTOC is still 1 TSTOC. Fees after the upgrade are paid
  in `atstoc`; the node minimum gas price becomes `1000000000atstoc` (`0.001utstoc` before). The EVM side is
  unchanged (it already used 18 decimals). Not yet scheduled on mainnet.

### Node configuration that changed with an upgrade

| From | Setting | Value |
|---|---|---|
| `v6.0.0` (607,280) | `minimum-gas-prices` (`app.toml` and `--minimum-gas-prices`) | `0.001utstoc` → `1000000000atstoc` |

## Past cycles

Each reset wiped the chain; nothing below can be replayed from today's genesis.

| Cycle | Genesis | Ended | Notes |
|---|---|---|---|
| 3 (current) | 2026-09-30 | — | genesis on `v5.0.1`; see above |
| 2 | 2026-06-10 | 2026-09-30 | fee reset: base fee 0.001 utstoc/gas (cycle 1 charged 1 utstoc/gas, ~167× mainnet) |
| 1 | 2026-06-09 | 2026-06-10 | redeploy with chain id `tstoc_1999-1` and EVM chain id 1999 |

Earlier testnets used different chain ids and are not tracked here.

## Running a testnet node

Join from the current genesis with the binary for the current head (`v6.0.0-rc3` now), or restore a snapshot.
Syncing from block 1 needs each governance binary in order, switching at each upgrade height, the same way as
on mainnet: `v5.0.1` up to 586,279 (it also replays the `v5.0.2` range, which changed no consensus code),
`v5.0.3` up to 607,279, then `v6.0.0-rc3`.
