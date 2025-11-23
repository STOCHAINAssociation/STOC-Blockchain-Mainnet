# STO Chain — `v1.2` source

This branch holds the source of the `v1.2` binary (adds MsgBurnToken). On mainnet (chain-id `stoc`) it executed blocks **2,709,242 – 4,455,466**.

It is kept so the chain can be replayed from genesis. To run a node on the current network, use the `main` branch.

## Build

Go toolchain: `go1.24.3`

```bash
git checkout phase/v1.2
go build -o stocd_v1.2 ./cmd/stocd
```

The replay order, the exact boundary heights and how to verify each range are in [HISTORY.md](../main/HISTORY.md) on `main`.
