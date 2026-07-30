# STO Chain — `v5.0.0` source

This branch holds the source of the `v5.0.0` binary (upgrade v5.0.0 (governance proposal #5)). On mainnet (chain-id `stoc`) it executed blocks **6,408,100 – head**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.25.8`

```bash
git checkout phase/v5.0.0
go build -o stocd_v5.0.0 ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
