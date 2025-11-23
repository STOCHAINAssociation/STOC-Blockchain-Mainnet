# Hướng Dẫn Frontend: Burn Token API

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

## Các Trường (Fields)

### 1. `creator` (string) - Required
Địa chỉ ví burn token (người sign message)

### 2. `denom` (string) - Required
- Native token: `"ustoc"` hoặc `"utstoc"` (testnet)
- Custom token: `minimal_denom` (ví dụ: `"mytoken"`)

### 3. `amount` (string) - Required khi `burn_all = false`
- Số lượng burn (atomic units)
- Bỏ qua khi `burn_all = true`
- Ví dụ: `"1000000"` = 1 token (decimals = 6)

### 4. `burn_all` (boolean) - Default: false
- `false`: Burn số lượng `amount`
- `true`: Burn toàn bộ số dư token

### 5. `include_gas_in_burn` (boolean) - Default: false
**Chỉ áp dụng cho native token (ustoc/utstoc)**

- `true`: Gas được trừ từ số lượng burn
  - Frontend tính: `amountToSend = userInput - estimatedGas`
  - User nhập 100 → Send 99.9 (đã trừ gas 0.1)

- `false`: User trả gas riêng, burn đúng số nhập
  - Frontend tính: `amountToSend = userInput`
  - User nhập 100 → Send 100 (gas 0.1 từ balance khác)

**Custom token**: Field này không có tác dụng (gas luôn trả bằng ustoc)

## Logic Frontend

### Tính Amount To Send

```javascript
// Estimate gas trước
const estimatedGas = await estimateGas();

let amountToSend;

if (burn_all) {
  amountToSend = "0"; // Backend bỏ qua
} else {
  const userInputAtomic = Decimal.fromUserInput(userInput, decimals).atomics;

  if (isNativeToken && include_gas_in_burn) {
    // Trừ gas từ số burn
    amountToSend = (BigInt(userInputAtomic) - BigInt(estimatedGas)).toString();
  } else {
    // Burn đúng số user nhập
    amountToSend = userInputAtomic;
  }
}
```

### Validate Balance

```javascript
if (isNativeToken && include_gas_in_burn) {
  // User chỉ cần đủ số muốn burn
  requiredBalance = userInputAtomic;
} else {
  // User cần số burn + gas
  requiredBalance = userInputAtomic + estimatedGas;
}

if (currentBalance < requiredBalance) {
  throw new Error("Số dư không đủ");
}
```

## UI Components

### 1. Token Dropdown
Hiển thị danh sách token + balance

### 2. Amount Input
- Disable khi `burn_all = true`
- Validate: > 0 và <= balance

### 3. Burn All Checkbox
- Default: unchecked
- Khi checked: disable amount input

### 4. Include Gas In Burn Checkbox
- **Chỉ hiện khi**: `denom` là native token (ustoc/utstoc)
- **Ẩn/disable**: Custom token
- **Hiện với**: `burn_all = true/false`
- Default: unchecked

### 5. Gas Fee Display
Hiển thị cảnh báo dựa vào `include_gas_in_burn`:
- `true`: "Gas sẽ được trừ từ số burn. Burn thực tế: {amount - gas}"
- `false`: "Cần thêm {gas} để trả phí. Tổng: {amount + gas}"

## Code Example

```javascript
import { Decimal } from "@cosmjs/math";

// User input
const userInput = "100"; // STOC
const denom = "ustoc";
const decimals = 6;
const isBurnAll = false;
const includeGasInBurn = false;

// Estimate gas
const estimatedGas = await estimateGasForBurn();

// Calculate amount
let amount = "0";
if (!isBurnAll) {
  const inputAtomic = Decimal.fromUserInput(userInput, decimals).atomics;

  if (denom === "ustoc" && includeGasInBurn) {
    // Trừ gas từ số burn
    amount = (BigInt(inputAtomic) - BigInt(estimatedGas)).toString();
  } else {
    amount = inputAtomic;
  }
}

// Create message
const msg = {
  typeUrl: "/stoc.stoc.MsgBurnToken",
  value: {
    creator: userAddress,
    amount: amount,
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

## Scenarios

### 1. Burn 100 ustoc, include_gas = true
- User input: 100
- Estimated gas: 0.1
- Amount send: 99.9 (đã trừ gas)
- Balance cần: 100
- Kết quả: Burn 99.9, còn 0.1

### 2. Burn 100 ustoc, include_gas = false
- User input: 100
- Estimated gas: 0.1
- Amount send: 100
- Balance cần: 100.1
- Kết quả: Burn 100, còn 0

### 3. Burn all 100 ustoc, include_gas = true
- Amount send: "0"
- Backend lấy balance sau gas
- Kết quả: Burn ~99.9, còn 0

### 4. Burn all 100 ustoc, include_gas = false
- Amount send: "0"
- Yêu cầu: User có thêm ustoc để trả gas
- Backend burn toàn bộ 100
- Kết quả: Burn 100, gas từ nguồn khác

### 5. Burn custom token
- `include_gas_in_burn` không có tác dụng
- Gas luôn trả bằng ustoc (riêng)
- Burn đúng số lượng yêu cầu

## Response Event

```
event: "burn_token"
attributes:
  - token_creator: địa chỉ burn
  - minimal_denom: token đã burn
  - burn_amount: số lượng thực tế burned
```

## API Endpoint

**POST** `/stoc.stoc.Msg/BurnToken`

OpenAPI: [docs/static/openapi.yml](../../docs/static/openapi.yml)

## Important Notes

⚠️ **Backend không xử lý `include_gas_in_burn`**
- Cosmos SDK trừ gas TRƯỚC KHI handler chạy
- Backend không biết gas fee là bao nhiêu
- **Frontend phải tự tính và trừ gas trước khi send**

⚠️ **Native token denom**
- Mainnet: `ustoc`
- Testnet: `utstoc`
- Check config/genesis để xác định
