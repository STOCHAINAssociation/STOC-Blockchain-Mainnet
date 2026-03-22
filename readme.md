# STO Chain — `v2-evm` source

This branch holds the source of the `v2-evm` binary (EVM upgrade (governance proposal #2)). On mainnet (chain-id `stoc`) it executed blocks **4,455,467 – 4,699,537**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.24.3`

```bash
git checkout phase/v2-evm
go build -o stocd_v2-evm ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
