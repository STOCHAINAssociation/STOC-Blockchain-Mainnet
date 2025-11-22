# Frontend Guide: Burn Token Feature

## Message Structure

**Type URL**: `/stoc.stoc.MsgBurnToken`

```json
{
  "creator": "stoc1...",
  "amount": "1000000",
  "denom": "ustoc",
  "burn_all": false,
  "include_gas_in_burn": false
}
```

## UI Fields

### 1. Token Dropdown
- Show user's token list with balance
- Value: `denom` (native) or `minimal_denom` (custom token)

### 2. Amount Input
- Disabled if `burn_all = true`
- Validate: > 0 and <= balance

### 3. Burn All Checkbox
- When checked: disable amount input

### 4. Include Gas in Burn Checkbox (NEW)
- **Only show for native token (`ustoc`)**
- **Disable for custom tokens**
- Label: "Use gas from burn amount"

## Logic

### Scenario 1: Burn Specific Amount
```javascript
{
  burn_all: false,
  amount: "100000000", // user input converted
  include_gas_in_burn: false // not important
}
```

### Scenario 2: Burn All Native Token
```javascript
{
  burn_all: true,
  amount: "0", // backend ignores this
  denom: "ustoc",
  include_gas_in_burn: true // show option for ustoc only
}
```
- If `include_gas_in_burn = true`: User needs extra ustoc for gas
- If `include_gas_in_burn = false`: Gas taken from burned amount

### Scenario 3: Burn All Custom Token
```javascript
{
  burn_all: true,
  amount: "0",
  denom: "mytoken", // minimal_denom
  include_gas_in_burn: false // always false, option disabled
}
```
- Burns all `mytoken`
- Gas paid in `ustoc` (separate)

## Code Example

```javascript
import { Decimal } from "@cosmjs/math";

const denom = "ustoc"; // or minimal_denom for custom token
const isBurnAll = false;
const includeGasInBurn = false;
const amountInput = "100";

// Convert amount
let amountStr = "0";
if (!isBurnAll) {
  const amount = Decimal.fromUserInput(amountInput, 6);
  amountStr = amount.atomics;
}

// Create message
const msg = {
  typeUrl: "/stoc.stoc.MsgBurnToken",
  value: {
    creator: userAddress,
    amount: amountStr,
    denom: denom,
    burn_all: isBurnAll,
    include_gas_in_burn: includeGasInBurn,
  },
};

// Send transaction
const result = await signingClient.signAndBroadcast(
  userAddress,
  [msg],
  fee
);
```

## UI/UX Rules

| Token Type | burn_all | include_gas_in_burn Option |
|-----------|----------|---------------------------|
| `ustoc` (native) | false | Hidden (not used) |
| `ustoc` (native) | true | **Enabled** - Show checkbox |
| Custom token | false | Hidden (not used) |
| Custom token | true | **Disabled** - Gas always from ustoc |

## Backend Behavior

**Important**: Cosmos SDK deducts gas BEFORE message execution.

- `GetBalance()` returns balance AFTER gas paid
- When `burn_all = true`: burns entire remaining balance
- Field `include_gas_in_burn` is for **UI display only**
- Backend doesn't need complex gas calculation

## Response Event

Event: `burn_token`
- `token_creator`: Burner address
- `minimal_denom`: Token burned
- `burn_amount`: Actual amount burned

## Implementation Checklist

- [ ] Token dropdown with balance
- [ ] Amount input (disabled on burn_all)
- [ ] Burn All checkbox
- [ ] Include Gas checkbox (show only for ustoc + burn_all)
- [ ] Validate amount <= balance
- [ ] Convert amount to atomic units
- [ ] Send transaction with all 5 fields
- [ ] Listen for burn_token event
- [ ] Update balance after success
