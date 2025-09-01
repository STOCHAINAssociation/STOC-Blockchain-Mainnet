TÔi muốn update code api golang, cụ thể như sau:

1. Endpoint `/cosmos/bank/v1beta1/balances/{address}` sẽ trả về data dạng:
    ```json
    {
        "balances": [
            {
            "denom": "KTC_4",
            "amount": "100000000"
            },
            {
            "denom": "MinhAnh_0",
            "amount": "136990000000000000000"
            },
            {
            "denom": "STOC_2_2",
            "amount": "99990000000000000000"
            },
            {
            "denom": "utstoc",
            "amount": "2992113"
            }
        ],
        "pagination": {
            "next_key": null,
            "total": "4"
        }
    }
    ```
    Issue của tôi với api này là: denom = minimal_denom và amount là `init_amount * (10^decimals)`. Khi call api get list balances ở 1 address thì k thể tính đống vì api k trả về decimals. Vậy giờ sửa lại api, tôi muốn khi call api balances thì sẽ return không chỉ denom và amount mà kèm cả metadata của token đó nữa, hiện tại có api get metadata là `​/stoc​/stoc​/tokens​/{minimal_denom}` nhưng chỉ trả 1, tôi cần all balances của 1 ví kém all info của token đó.


Note:
Lưu ý: Source đang dùng Cosmos SDK + Ignite CLI -> các api này default của cosmos, giờ tôi cần custom trên chính api đó nhưng ignite đang deep_import chứ k có code của api (theo tôi hiểu là vậy)