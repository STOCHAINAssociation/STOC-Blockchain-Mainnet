# STOC Blockchain API Documentation

This document provides comprehensive API documentation for the STOC blockchain, including REST API, RPC, gRPC, and WebSocket endpoints.

## Base URLs

- **Mainnet REST API**: `https://api-stoc-mainnet.stochainscan.io`
- **Mainnet RPC**: `https://api-stoc-mainnet.stochainscan.io/rpc`
- **Local Development**: `http://localhost:1317` (REST), `http://localhost:26657/rpc` (RPC)

## Authentication

Most read operations don't require authentication. Write operations require transaction signing with a valid private key.

## RPC Endpoints (Tendermint)

All RPC endpoints use the `/rpc` prefix. These are direct Tendermint RPC calls.

### Node Information

#### Get ABCI Info
```http
GET /rpc/abci_info
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": -1,
  "result": {
    "response": {
      "data": "stoc",
      "version": "1.0.0",
      "app_version": "1",
      "last_block_height": "12345",
      "last_block_app_hash": "ABC123..."
    }
  }
}
```

#### ABCI Query
```http
GET /rpc/abci_query?path={path}&data={data}&height={height}&prove={prove}
```

**Parameters:**
- `path`: Query path (e.g., `"/store/bank/key"`)
- `data`: Query data (hex encoded)
- `height`: Block height (optional, default: latest)
- `prove`: Include merkle proof (optional, default: false)

**Example:**
```http
GET /rpc/abci_query?path="/cosmos.bank.v1beta1.Query/Balance"&data=0x...&height=12345&prove=false
```

#### Get Status
```http
GET /rpc/status
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": -1,
  "result": {
    "node_info": {
      "protocol_version": {
        "p2p": "8",
        "block": "11",
        "app": "0"
      },
      "id": "node_id",
      "listen_addr": "tcp://0.0.0.0:26656",
      "network": "stoc",
      "version": "0.37.2",
      "channels": "40202122233038606100",
      "moniker": "node_moniker"
    },
    "sync_info": {
      "latest_block_hash": "ABC123...",
      "latest_app_hash": "DEF456...",
      "latest_block_height": "12345",
      "latest_block_time": "2023-01-01T00:00:00Z",
      "earliest_block_hash": "GHI789...",
      "earliest_app_hash": "JKL012...",
      "earliest_block_height": "1",
      "earliest_block_time": "2023-01-01T00:00:00Z",
      "catching_up": false
    },
    "validator_info": {
      "address": "validator_address",
      "pub_key": {
        "type": "tendermint/PubKeyEd25519",
        "value": "pub_key_value"
      },
      "voting_power": "1000000"
    }
  }
}
```

#### Get Network Info
```http
GET /rpc/net_info
```

### Blocks and Transactions

#### Get Block by Height
```http
GET /rpc/block?height={height}
```

**Parameters:**
- `height`: Block height (optional, default: latest)

**Example:**
```http
GET /rpc/block?height=12345
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": -1,
  "result": {
    "block_id": {
      "hash": "ABC123...",
      "parts": {
        "total": 1,
        "hash": "DEF456..."
      }
    },
    "block": {
      "header": {
        "version": {
          "block": "11"
        },
        "chain_id": "stoc",
        "height": "12345",
        "time": "2023-01-01T00:00:00Z",
        "last_block_id": {
          "hash": "GHI789...",
          "parts": {
            "total": 1,
            "hash": "JKL012..."
          }
        },
        "last_commit_hash": "MNO345...",
        "data_hash": "PQR678...",
        "validators_hash": "STU901...",
        "next_validators_hash": "VWX234...",
        "consensus_hash": "YZA567...",
        "app_hash": "BCD890...",
        "last_results_hash": "EFG123...",
        "evidence_hash": "HIJ456...",
        "proposer_address": "KLM789..."
      },
      "data": {
        "txs": []
      },
      "evidence": {
        "evidence": []
      },
      "last_commit": {
        "height": "12344",
        "round": 0,
        "block_id": {
          "hash": "NOP012...",
          "parts": {
            "total": 1,
            "hash": "QRS345..."
          }
        },
        "signatures": []
      }
    }
  }
}
```

#### Get Latest Block
```http
GET /rpc/block
```

#### Get Block Results
```http
GET /rpc/block_results?height={height}
```

#### Get Transaction by Hash
```http
GET /rpc/tx?hash={hash}&prove={prove}
```

**Parameters:**
- `hash`: Transaction hash (hex encoded with 0x prefix)
- `prove`: Include merkle proof (optional, default: false)

**Example:**
```http
GET /rpc/tx?hash=0xABC123...&prove=false
```

#### Search Transactions
```http
GET /rpc/tx_search?query={query}&prove={prove}&page={page}&per_page={per_page}&order_by={order_by}
```

**Parameters:**
- `query`: Search query (e.g., `"tx.height=12345"`)
- `prove`: Include merkle proof (optional, default: false)
- `page`: Page number (optional, default: 1)
- `per_page`: Results per page (optional, default: 30, max: 100)
- `order_by`: Sort order - "asc" or "desc" (optional, default: "asc")

**Example:**
```http
GET /rpc/tx_search?query="message.sender='stoc1...'"&page=1&per_page=10
```

### Consensus and Validators

#### Get Validators
```http
GET /rpc/validators?height={height}&page={page}&per_page={per_page}
```

#### Get Consensus State
```http
GET /rpc/consensus_state
```

#### Get Dump Consensus State
```http
GET /rpc/dump_consensus_state
```

### Mempool

#### Get Mempool
```http
GET /rpc/unconfirmed_txs?limit={limit}
```

#### Get Number of Unconfirmed Transactions
```http
GET /rpc/num_unconfirmed_txs
```

### Genesis

#### Get Genesis
```http
GET /rpc/genesis
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": -1,
  "result": {
    "genesis": {
      "genesis_time": "2023-01-01T00:00:00Z",
      "chain_id": "stoc",
      "initial_height": "1",
      "consensus_params": {
        "block": {
          "max_bytes": "22020096",
          "max_gas": "-1",
          "time_iota_ms": "1000"
        },
        "evidence": {
          "max_age_num_blocks": "100000",
          "max_age_duration": "172800000000000",
          "max_bytes": "1048576"
        },
        "validator": {
          "pub_key_types": ["ed25519"]
        },
        "version": {}
      },
      "app_hash": "",
      "app_state": {
        // Genesis app state
      },
      "validators": []
    }
  }
}
```

## REST API Endpoints

### Node Information

#### Get Node Info
```http
GET /cosmos/base/tendermint/v1beta1/node_info
```

**Response:**
```json
{
  "default_node_info": {
    "protocol_version": {
      "p2p": "8",
      "block": "11",
      "app": "0"
    },
    "default_node_id": "node_id",
    "listen_addr": "tcp://0.0.0.0:26656",
    "network": "stoc",
    "version": "0.37.2",
    "channels": "40202122233038606100",
    "moniker": "node_moniker"
  }
}
```

#### Get Syncing Status
```http
GET /cosmos/base/tendermint/v1beta1/syncing
```

**Response:**
```json
{
  "syncing": false
}
```

### Blocks and Transactions

#### Get Latest Block
```http
GET /cosmos/base/tendermint/v1beta1/blocks/latest
```

#### Get Block by Height
```http
GET /cosmos/base/tendermint/v1beta1/blocks/{height}
```

#### Get Transaction by Hash
```http
GET /cosmos/tx/v1beta1/txs/{hash}
```

#### Search Transactions
```http
GET /cosmos/tx/v1beta1/txs?events={events}&pagination.limit={limit}
```

**Parameters:**
- `events`: Event filters (e.g., `message.sender='address'`)
- `pagination.limit`: Number of results to return

### Bank Module

#### Get Account Balance
```http
GET /cosmos/bank/v1beta1/balances/{address}
```

**Response:**
```json
{
  "balances": [
    {
      "denom": "ustoc",
      "amount": "1000000"
    }
  ],
  "pagination": {
    "next_key": null,
    "total": "1"
  }
}
```

#### Get Balance by Denomination
```http
GET /cosmos/bank/v1beta1/balances/{address}/by_denom?denom={denom}
```

#### Get Total Supply
```http
GET /cosmos/bank/v1beta1/supply
```

#### Get Supply by Denomination
```http
GET /cosmos/bank/v1beta1/supply/by_denom?denom={denom}
```

### Staking Module

#### Get All Validators
```http
GET /cosmos/staking/v1beta1/validators
```

#### Get Validator by Address
```http
GET /cosmos/staking/v1beta1/validators/{validator_addr}
```

#### Get Delegations
```http
GET /cosmos/staking/v1beta1/delegations/{delegator_addr}
```

#### Get Validator Delegations
```http
GET /cosmos/staking/v1beta1/validators/{validator_addr}/delegations
```

#### Get Unbonding Delegations
```http
GET /cosmos/staking/v1beta1/delegators/{delegator_addr}/unbonding_delegations
```

### Distribution Module

#### Get Delegation Rewards
```http
GET /cosmos/distribution/v1beta1/delegators/{delegator_address}/rewards
```

#### Get Validator Commission
```http
GET /cosmos/distribution/v1beta1/validators/{validator_address}/commission
```

### Governance Module

#### Get All Proposals
```http
GET /cosmos/gov/v1beta1/proposals
```

#### Get Proposal by ID
```http
GET /cosmos/gov/v1beta1/proposals/{proposal_id}
```

#### Get Proposal Votes
```http
GET /cosmos/gov/v1beta1/proposals/{proposal_id}/votes
```

### Custom STOC Module

#### Get STOC Parameters
```http
GET /stoc/stoc/params
```

#### Get STOC Module State
```http
GET /stoc/stoc/state
```

## gRPC API

### Connection Setup

```go
import (
    "google.golang.org/grpc"
    "github.com/cosmos/cosmos-sdk/types/query"
)

conn, err := grpc.Dial("localhost:9090", grpc.WithInsecure())
defer conn.Close()
```

### Bank Queries

```go
import banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

bankClient := banktypes.NewQueryClient(conn)

// Get balance
balanceReq := &banktypes.QueryBalanceRequest{
    Address: "stoc1...",
    Denom:   "ustoc",
}
balanceRes, err := bankClient.Balance(context.Background(), balanceReq)
```

### Staking Queries

```go
import stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

stakingClient := stakingtypes.NewQueryClient(conn)

// Get validators
validatorsReq := &stakingtypes.QueryValidatorsRequest{
    Pagination: &query.PageRequest{Limit: 100},
}
validatorsRes, err := stakingClient.Validators(context.Background(), validatorsReq)
```

## WebSocket API (Tendermint RPC)

### Connection

```javascript
const ws = new WebSocket('wss://api-stoc-mainnet.stochainscan.io/rpc/websocket');
```

### Subscribe to New Blocks

```json
{
  "jsonrpc": "2.0",
  "method": "subscribe",
  "id": 1,
  "params": {
    "query": "tm.event='NewBlock'"
  }
}
```

### Subscribe to Transactions

```json
{
  "jsonrpc": "2.0",
  "method": "subscribe",
  "id": 2,
  "params": {
    "query": "tm.event='Tx'"
  }
}
```

### Subscribe to Validator Set Updates

```json
{
  "jsonrpc": "2.0",
  "method": "subscribe",
  "id": 3,
  "params": {
    "query": "tm.event='ValidatorSetUpdates'"
  }
}
```

## Transaction Broadcasting

### Broadcast Transaction (RPC)

```http
POST /rpc/broadcast_tx_sync
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "broadcast_tx_sync",
  "params": {
    "tx": "base64_encoded_transaction"
  }
}
```

### Broadcast Transaction (Async)

```http
POST /rpc/broadcast_tx_async
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "broadcast_tx_async",
  "params": {
    "tx": "base64_encoded_transaction"
  }
}
```

### Broadcast Transaction (Commit)

```http
POST /rpc/broadcast_tx_commit
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "broadcast_tx_commit",
  "params": {
    "tx": "base64_encoded_transaction"
  }
}
```

### Broadcast Transaction (REST)

```http
POST /cosmos/tx/v1beta1/txs
Content-Type: application/json

{
  "tx_bytes": "base64_encoded_transaction",
  "mode": "BROADCAST_MODE_SYNC"
}
```

## Client Libraries

### JavaScript/TypeScript

```bash
npm install @cosmjs/stargate @cosmjs/proto-signing
```

```typescript
import { StargateClient, SigningStargateClient } from "@cosmjs/stargate";
import { DirectSecp256k1HdWallet } from "@cosmjs/proto-signing";

// Query client
const client = await StargateClient.connect("https://api-stoc-mainnet.stochainscan.io/rpc");

// Get balance
const balance = await client.getBalance("stoc1...", "ustoc");

// Signing client
const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic);
const signingClient = await SigningStargateClient.connectWithSigner(
  "https://api-stoc-mainnet.stochainscan.io/rpc",
  wallet
);

// Send tokens
const result = await signingClient.sendTokens(
  senderAddress,
  recipientAddress,
  [{ denom: "ustoc", amount: "1000000" }],
  "auto"
);
```

### Python

```bash
pip install cosmpy
```

```python
from cosmpy.aerial.client import LedgerClient
from cosmpy.aerial.wallet import LocalWallet

# Create client
client = LedgerClient("https://api-stoc-mainnet.stochainscan.io/rpc")

# Create wallet
wallet = LocalWallet.from_mnemonic(mnemonic)

# Get balance
balance = client.query_bank_balance(wallet.address(), "ustoc")

# Send transaction
tx = client.send_tokens(
    wallet.address(),
    recipient_address,
    [{"denom": "ustoc", "amount": "1000000"}],
    wallet
)
```

### Go

```go
import (
    "github.com/cosmos/cosmos-sdk/client"
    "github.com/cosmos/cosmos-sdk/client/tx"
    "github.com/cosmos/cosmos-sdk/types"
)

// Create client context
clientCtx := client.Context{}.
    WithNodeURI("https://api-stoc-mainnet.stochainscan.io/rpc").
    WithChainID("stoc")

// Query balance
bankClient := banktypes.NewQueryClient(clientCtx)
balance, err := bankClient.Balance(context.Background(), &banktypes.QueryBalanceRequest{
    Address: "stoc1...",
    Denom:   "ustoc",
})
```

## Error Handling

### Common HTTP Status Codes

- `200`: Success
- `400`: Bad Request - Invalid parameters
- `404`: Not Found - Resource doesn't exist
- `500`: Internal Server Error
- `503`: Service Unavailable - Node is syncing

### Error Response Format

```json
{
  "code": 3,
  "message": "account sequence mismatch, expected 10, got 9",
  "details": []
}
```

## Rate Limiting

- Public endpoints: 100 requests per minute per IP
- No rate limiting for local development nodes
- Consider running your own node for high-frequency applications

## Pagination

Most list endpoints support pagination:

```http
GET /cosmos/staking/v1beta1/validators?pagination.limit=50&pagination.offset=100
```

**Parameters:**
- `pagination.limit`: Number of items per page (max 100)
- `pagination.offset`: Number of items to skip
- `pagination.key`: Base64-encoded key for cursor-based pagination

## WebSocket Events

### Block Events

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "query": "tm.event='NewBlock'",
    "data": {
      "type": "tendermint/event/NewBlock",
      "value": {
        "block": {
          "header": {
            "height": "12345",
            "time": "2023-01-01T00:00:00Z"
          }
        }
      }
    }
  }
}
```

### Transaction Events

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "query": "tm.event='Tx'",
    "data": {
      "type": "tendermint/event/Tx",
      "value": {
        "TxResult": {
          "height": "12345",
          "hash": "ABC123...",
          "result": {
            "code": 0,
            "events": []
          }
        }
      }
    }
  }
}
```

## Testing

### Testnet Endpoints

- **Testnet REST API**: `https://api-testnet-stoc.stochainscan.io`
- **Testnet RPC**: `https://rpc-testnet-stoc.stochainscan.io/rpc`

### Local Testing

```bash
# Start local node
stocd start --api.enable=true --api.swagger=true

# Test RPC endpoints
curl http://localhost:26657/rpc/status
curl http://localhost:26657/rpc/abci_info

# Test REST endpoints
curl http://localhost:1317/cosmos/base/tendermint/v1beta1/node_info
```

## Support

For API support and questions:
- GitHub Issues: https://github.com/STOCHAINAssociation/STOC-Blockchain-Mainnet/issues
- Documentation: Check the repository's docs folder

---

**Note**: This API documentation is for STOC blockchain mainnet. Always verify endpoint availability and response formats with the latest version. 