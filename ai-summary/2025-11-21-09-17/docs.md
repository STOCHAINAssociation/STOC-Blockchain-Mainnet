# Hướng dẫn tích hợp Frontend: Tính năng Burn Token

Tài liệu này hướng dẫn FE xây dựng form và kết nối với blockchain để thực hiện tính năng Burn Token (Đốt token).

## 1. Tổng quan

Tính năng cho phép người dùng đốt (xóa bỏ vĩnh viễn) một lượng token nhất định hoặc toàn bộ số dư token của họ.
Hỗ trợ:

- Token native của chain (`stoc`).
- Token được tạo ra từ module `stoc` (ví dụ: `token_abc`).

## 2. Yêu cầu UI/UX (Form Burn Token)

Form cần có các trường sau:

1.  **Chọn Token (Dropdown/Select)**:

    - Hiển thị danh sách token người dùng đang sở hữu.
    - Giá trị cần lấy: `denom` (ví dụ: `ustoc`, `token_abc`).
    - Hiển thị số dư hiện tại (Balance) để người dùng biết.

2.  **Tùy chọn Burn All (Checkbox/Switch)**:

    - Label: "Burn tất cả token" (Burn All).
    - Logic: Khi bật, disable trường "Số lượng" (Amount).

3.  **Số lượng (Input Number)**:

    - Label: "Số lượng muốn burn".
    - Validation:
      - Phải > 0.
      - Phải <= Số dư hiện tại.
    - Disabled nếu chọn "Burn All".

4.  **Hiển thị Fee (Estimated)**:

    - Hiển thị phí giao dịch dự kiến (Gas fee).

5.  **Nút Submit**:
    - Label: "Burn Token".

## 3. Kết nối Blockchain

### Message Proto

Message cần gửi lên chain là `MsgBurnToken`.

**Type URL**: `/stoc.stoc.MsgBurnToken`

**Cấu trúc Message**:

```json
{
  "creator": "stoc1...", // Địa chỉ ví người dùng (lấy từ wallet đang connect)
  "amount": "1000000", // Số lượng muốn burn (tính theo đơn vị nhỏ nhất, ví dụ 1 STOC = 10^6 ustoc)
  "denom": "ustoc", // Mã token (denom)
  "burn_all": false // true nếu chọn Burn All, false nếu nhập số lượng
}
```

### Logic xử lý

#### Trường hợp 1: Burn một lượng cụ thể (Burn Specific Amount)

- **Input**: Người dùng nhập số lượng `X`.
- **Payload**:
  - `amount`: `X` (đã quy đổi ra đơn vị nhỏ nhất).
  - `burn_all`: `false`.
- **Fee**: Người dùng trả phí giao dịch `Fee` riêng. Tổng tài sản bị trừ: `X + Fee`.

#### Trường hợp 2: Burn tất cả (Burn All)

- **Input**: Người dùng chọn "Burn All".
- **Payload**:
  - `amount`: Có thể gửi `0` hoặc bất kỳ số nào (Backend sẽ tự lấy số dư thực tế).
  - `burn_all`: `true`.
- **Lưu ý về Fee**:
  - Backend sẽ tự động lấy toàn bộ số dư của `denom` đó để burn.
  - Nếu burn token native (`stoc`) dùng để trả fee: Backend sẽ burn (Số dư - Fee).
  - Nếu burn token khác: Backend burn toàn bộ số dư token đó, Fee trừ vào token native (`stoc`).

### Ví dụ Code (Sử dụng CosmJS/Keplr)

```javascript
import { Decimal } from "@cosmjs/math";

// 1. Chuẩn bị thông tin
const userAddress = "stoc1..."; // Lấy từ wallet
const denom = "ustoc"; // Token được chọn
const isBurnAll = false; // Giá trị từ checkbox
const amountInput = "100"; // Giá trị từ input (ví dụ 100 STOC)

// 2. Quy đổi amount (nếu không phải burn all)
// Giả sử decimals = 6
let amountStr = "0";
if (!isBurnAll) {
  const amount = Decimal.fromUserInput(amountInput, 6);
  amountStr = amount.atomics; // "100000000"
}

// 3. Tạo Message
const msg = {
  typeUrl: "/stoc.stoc.MsgBurnToken",
  value: {
    creator: userAddress,
    amount: amountStr,
    denom: denom,
    burn_all: isBurnAll,
  },
};

// 4. Gửi Transaction
const fee = {
  amount: [{ denom: "ustoc", amount: "500" }],
  gas: "200000",
};

const result = await signingClient.signAndBroadcast(userAddress, [msg], fee);
console.log("Tx Hash:", result.transactionHash);
```

## 4. Phản hồi (Response)

Sau khi transaction thành công, Backend sẽ trả về event `burn_token` với các thuộc tính:

- `token_creator`: Địa chỉ người burn.
- `minimal_denom`: Token bị burn.
- `burn_amount`: Số lượng thực tế đã burn.

FE có thể dựa vào Tx Hash để query lại kết quả hoặc lắng nghe event để cập nhật lại số dư cho người dùng.
