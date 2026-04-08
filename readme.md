# STO Chain — `v3` source

This branch holds the source of the `v3` binary (upgrade v3-fix-evm-denom (governance proposal #4)). On mainnet (chain-id `stoc`) it executed blocks **4,705,316 – 4,794,076**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.24.3`

```bash
git checkout phase/v3
go build -o stocd_v3 ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
