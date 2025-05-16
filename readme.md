# STO Chain — `v1.1` source

This branch holds the source of the `v1.1` binary (cosmos-sdk v0.53.0, CometBFT 0.38.17). On mainnet (chain-id `stoc`) it executed blocks **542,405 – 2,709,241**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.24.3`

```bash
git checkout phase/v1.1
go build -o stocd_v1.1 ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
