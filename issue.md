# STOChain — EVM Security Review & Upgrade Planning

> **Created:** 2026-04-02
> **Updated:** 2026-04-02 (merged with automated audit results)
> **Sources:** CVE-2026-ASA002, cosmos/evm changelog, PANews disclosure, DailyCVE, govulncheck, Dependabot, manual code audit

---

## TOM TAT TINH HINH

STOChain dang chay EVM module version **cosmos/evm v1.0.0-rc2** (fork tai `github.com/MinhAnh-Corp/evm`).

### Status da xac nhan (tu source code audit):

| Item | Status | Chi tiet |
|------|--------|----------|
| EVM module | `github.com/cosmos/evm` v1.0.0-rc2 (fork MinhAnh-Corp) | **go.mod confirmed** |
| ICS20 precompile | **DISABLED** | Chi co bech32 + p256 (stateless) |
| CVE-2026-ASA002 | **KHONG BI TRUC TIEP** | ICS20 precompile khong register |
| Audit status | **KHONG** — v1.0.0-rc chua bao gio duoc audit | Sherlock audit chi cover v0.3.0+ |
| Security patches | **THIEU 236 commits** | Diverge truoc audit, khong co post-audit fixes |
| Branch status | **DEAD-END** | Chi 2 chains dung (STOC + 1), da bi abandoned |

---

## CVE-2026-ASA002 — Critical (ICS20 Precompile)

### Thong tin

| Field | Value |
|-------|-------|
| **CVE ID** | CVE-2026-ASA002 |
| **Severity** | CRITICAL |
| **Affected** | Cosmos EVM pre-v0.6.0 (tat ca chains co ICS20 enabled) |
| **Discovered** | Thang 1/2026 |
| **Real-world loss** | $7M (Saga network, 21/1/2026) |
| **STOC Impact** | **KHONG BI** — ICS20 precompile KHONG duoc register trong `app/evm.go` |

### Root Cause

Loi nam o **ICS20 precompile** trong nested EVM execution path:

```
Attacker Contract
    -> goi ICS20 precompile (0x0000...0802)
        -> transfer asset (EXECUTED)
    -> revert state contract ve gia tri goc
        -> dirtyStorage == originStorage
            -> StateDB.Commit() BO QUA ghi KVStore
                -> balance deduction KHONG duoc ghi
                -> DOUBLE SPEND / UNAUTHORIZED MINT
```

### STOC Configuration (confirmed from source)

```go
// app/evm.go — Chi register 2 precompile (KHONG co ICS20)
precompiles := maps.Clone(gethvm.PrecompiledContractsPrague)
precompiles[bech32Precompile.Address()] = bech32Precompile  // stateless
precompiles[p256Precompile.Address()] = p256Precompile      // stateless
```

**Ket luan: STOC an toan voi CVE-2026-ASA002 cu the, nhung StateDB bug co the anh huong cac execution path khac.**

---

## STOC-SPECIFIC VULNERABILITIES (Audit Results)

### CRITICAL — Da fix (commit f032b64 tren feat/evm)

| # | Issue | Impact | Fix |
|---|-------|--------|-----|
| 1 | ICA Host bypass IBC restriction | Custom tokens escape via IBC qua ICA execution | BankKeeper SendRestriction |
| 2 | x/group bypass IBC restriction | Custom tokens escape via group proposal | BankKeeper SendRestriction |
| 3 | x/gov bypass IBC restriction | Custom tokens escape via governance proposal | BankKeeper SendRestriction |

### HIGH — Chua fix (can upgrade cosmos/evm)

| # | Issue | Impact | Fix |
|---|-------|--------|-----|
| 4 | eth_getLogs OOM (cosmos/evm #1033) | 1 RPC request crash node, zero cost | Upgrade to v0.6.0 |
| 5 | RPCStream panic (cosmos/evm #1037) | Network blip crash node | Upgrade to v0.6.0 |
| 6 | Mempool race conditions (#656, #658) | Consensus failure under load | Upgrade to v0.6.0 |
| 7 | Non-deterministic pre-blocker (#729) | Chain halt — validators produce different state | Upgrade to v0.6.0 |
| 8 | Gas price bypass (#657) | Spam transactions below min gas price | Upgrade to v0.6.0 |

### MEDIUM — Da fix (commit f032b64)

| # | Issue | Fix |
|---|-------|-----|
| 9 | detectBondDenomFromGenesis config.toml heuristic → wrong denom | Removed heuristic |
| 10 | MaxTxGasWanted = 0 → single-tx block monopolization | Default 50M |
| 11 | CreateToken negative remainder → chain halt panic | IsNegative guard |

---

## DEPENDENCY SCAN RESULTS

### govulncheck — 8 confirmed vulnerabilities in code paths

| # | ID | Package | Current | Fix | Severity |
|---|---|---|---|---|---|
| 1 | GO-2026-4603 | html/template (stdlib) | go1.24.13 | go1.25.8 | MEDIUM |
| 2 | GO-2026-4602 | os (stdlib) | go1.24.13 | go1.25.8 | MEDIUM |
| 3 | GO-2026-4601 | net/url (stdlib) | go1.24.13 | go1.25.8 | MEDIUM |
| 4 | GO-2025-4087 | consensys/gnark-crypto | v0.18.0 | v0.18.1 | HIGH |
| 5 | GO-2025-3922 | ulikunitz/xz | v0.5.14 | v0.5.15 | MEDIUM |
| 6 | GO-2026-4479 | pion/dtls/v2 | v2.2.7 | No fix | HIGH |
| 7 | GO-2023-1881 | cosmos-sdk x/crisis | v0.53.4 | N/A | LOW |
| 8 | GO-2023-1821 | cosmos-sdk x/crisis | v0.53.4 | N/A | LOW |

### go-ethereum CVEs (cosmos/go-ethereum v1.16.2-cosmos-1)

| CVE | Severity | Impact | Fixed In |
|-----|----------|--------|----------|
| CVE-2026-22868 | HIGH | DoS via crafted p2p message → high CPU | v1.16.8 |
| CVE-2026-26313 | HIGH | DoS via p2p message → high memory | v1.17.0 |
| CVE-2026-26314 | HIGH | DoS via network message → node crash | v1.16.9 |
| CVE-2026-26315 | MEDIUM | ECIES flaw → partial p2p key extraction | v1.16.9 |

**Note:** STOC dung CometBFT p2p, KHONG dung Ethereum devp2p. Risk thuc te thap hon severity rating, nhung van can update.

### Outdated Critical Dependencies

| Package | Current | Target | Priority | Reason |
|---------|---------|--------|----------|--------|
| cosmos/evm | v1.0.0-rc2 | **v0.6.0** | **CRITICAL** | Dead-end, no patches, missing 236 commits |
| cosmos-sdk | v0.53.4 | v0.53.6 | HIGH | Chain halt fix (x/group), validator rewards overflow |
| ibc-go | v10.2.0 | v10.5.0 | HIGH | Non-deterministic JSON unmarshalling → chain halt |
| gnark-crypto | v0.18.0 | v0.18.1 | HIGH | Memory allocation crash (GO-2025-4087) |
| go-ethereum fork | v1.16.2-cosmos-1 | v1.16.9+ | HIGH | 4 CVEs |
| jose2go | v1.7.0 | v1.8.0 | HIGH | JWT bomb attack |
| go-getter | v1.7.9 | v1.8.5 | HIGH | Symlink attack |
| bbolt | v1.4.0-alpha.1 | v1.4.3 | MEDIUM | Alpha in production |

### Dependencies OK (khong can action)

| Package | Version | Status |
|---------|---------|--------|
| CometBFT | v0.38.21 | All advisories patched (CSA-2026-001, ASA-2025-003, ASA-2025-002) |
| grpc | v1.79.3 | Exact fix version for CVE-2026-33186 |
| golang.org/x/crypto | v0.46.0 | All CVEs patched |
| golang.org/x/net | v0.48.0 | All CVEs patched |
| btcsuite/btcd | v0.24.2 | All CVEs patched |

### Replace Directives (go.mod)

| Package | Replaced With | Risk |
|---------|--------------|------|
| cosmos/evm | MinhAnh-Corp/evm v1.0.0-rc2 fork | **CRITICAL** — no upstream patches |
| go-ethereum | cosmos/go-ethereum v1.16.2-cosmos-1 | HIGH — 4 CVEs, can update |
| gin-gonic/gin | v1.9.1 (pinned) | LOW — security fix pin |
| syndtr/goleveldb | pinned commit | LOW — standard Cosmos fix |
| nhooyr.io/websocket | coder/websocket v1.8.7 | LOW — vanity URL fix |

---

## BLOCKERS — DA XAC NHAN

| Question | Answer |
|----------|--------|
| EVM module dang dung repo nao? | `github.com/cosmos/evm` v1.0.0-rc2, fork tai `github.com/MinhAnh-Corp/evm` |
| ICS20 precompile co bat khong? | **KHONG** — chi bech32 + p256 |
| Co dung ICS20/IBC cross-chain qua EVM? | **KHONG** — custom tokens blocked from IBC (SendRestriction) |
| Devnet ready? | **CO** — `ignite chain serve --reset-once` |
| Validators mainnet? | Can confirm so luong, coordinate upgrade |

---

## PRIORITY ACTIONS

| Priority | Action | Status | Timeline |
|----------|--------|--------|----------|
| P0 | Kiem tra ICS20 precompile | **DONE — DISABLED** | Done |
| P0 | Fix IBC bypass (ICA/Group/Gov) | **DONE — f032b64** | Done |
| P0 | Deploy security fixes (feat/evm binary) | **READY** | Deploy now |
| P1 | Migrate cosmos/evm v1.0.0-rc2 → v0.6.0 | **IN PROGRESS** | Branch created |
| P1 | Bump cosmos-sdk v0.53.4 → v0.53.6 | Pending | With EVM migration |
| P1 | Bump ibc-go v10.2.0 → v10.5.0 | Pending | With EVM migration |
| P1 | Bump gnark-crypto v0.18.0 → v0.18.1 | Pending | With EVM migration |
| P1 | Update go-ethereum fork → v1.16.9+ | Pending | With EVM migration |
| P2 | Enable feemarket (NoBaseFee=false) | Pending | Governance proposal |
| P2 | Rate-limit eth_getLogs at reverse proxy | Pending | VPS config |
| P3 | Bump jose2go, go-getter, bbolt | Pending | With EVM migration |
| P3 | Upgrade Go toolchain → 1.25.8+ | Pending | After SDK compat check |

---

## VPS HARDENING (IMMEDIATE)

```bash
# 1. Block JSON-RPC neu khong can public
sudo ufw deny 8545
sudo ufw deny 8546

# 2. Whitelist IP cho JSON-RPC
sudo ufw allow from <YOUR_IP> to any port 8545

# 3. Rate limit eth_getLogs (nginx example)
# location /eth {
#     limit_req zone=rpc burst=10 nodelay;
# }

# 4. Auto-restart on crash
# [Service]
# Restart=always
# RestartSec=5

# 5. Ports can thiet
# 26656 - P2P (keep open)
# 26657 - Tendermint RPC (restrict)
# 1317  - REST API (restrict)
```

---

## REFERENCE REPOS (cho migration)

```bash
# cosmos/evm official example app
git clone --branch v0.6.0 --depth 1 https://github.com/cosmos/evm.git ~/refs/cosmos-evm-v0.6.0

# Warden Protocol — cleanest v0.6.0 integration
git clone --depth 1 https://github.com/warden-protocol/wardenprotocol.git ~/refs/wardenprotocol
```

---

## REFERENCES

| Source | Link |
|--------|------|
| CVE-2026-ASA002 | https://dailycve.com/cosmos-evm-state-handling-vulnerability-cve-2026-asa002-critical/ |
| GitHub Advisory (GHSA-54gx-3cgr-7mfm) | https://github.com/advisories/GHSA-54gx-3cgr-7mfm |
| cosmos/evm repo | https://github.com/cosmos/evm |
| cosmos/evm security | https://github.com/cosmos/evm/security |
| Cosmos EVM docs | https://evm.cosmos.network |
| Cosmos Stack Roadmap 2026 | https://www.cosmoslabs.io/blog/the-cosmos-stack-roadmap-2026 |
| Saga incident (PANews) | https://www.panewslab.com/en/articles/019cd5aa-0e9d-7296-95fd-3bf7ac1367c7 |
| CertiK EVM-Cosmos Research | https://www.certik.com/resources/blog/evm-cosmos-convergence-research-from-security-base-part-2 |
| Halborn Cosmos EVM Audit | https://www.halborn.com/audits/tac/cosmos-evm-edf06b |
| go-ethereum CVEs | https://geth.ethereum.org/docs/developers/geth-developer/disclosures |
| CometBFT Advisories | https://github.com/cometbft/cometbft/security/advisories |
| Cosmos SDK Advisories | https://github.com/cosmos/cosmos-sdk/security/advisories |
| IBC-Go Advisories | https://github.com/cosmos/ibc-go/security/advisories |
