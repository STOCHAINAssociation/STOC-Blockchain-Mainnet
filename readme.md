# STO Chain — `v3.1` source

This branch holds the source of the `v3.1` binary (removes the token creation fee). On mainnet (chain-id `stoc`) it executed blocks **4,794,077 – 6,408,099**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.24.3`

```bash
git checkout phase/v3.1
go build -o stocd_v3.1 ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
