#!/usr/bin/env python3
"""
Aster v3 agent signature helper.

Reads JSON on stdin:
{
  "msg": "<payload string>",
  "user": "0x...",
  "signer": "0x...",
  "nonce": "123",
  "private_key": "0x...",
  "chain_id": "714"
}

Prints signature hex (0x-prefixed) to stdout.
"""

import json
import sys

from eth_account import Account
from eth_account.messages import encode_typed_data


def main() -> int:
    payload = json.load(sys.stdin)

    msg = str(payload.get("msg", "")).strip()
    user = str(payload.get("user", "")).strip()
    signer = str(payload.get("signer", "")).strip()
    nonce = int(str(payload.get("nonce", "0")).strip())
    private_key = str(payload.get("private_key", "")).strip()
    chain_id = int(str(payload.get("chain_id", "0")).strip())

    if not msg or not user or not signer or nonce <= 0 or not private_key or chain_id <= 0:
        raise ValueError("missing required fields for agent signing")

    typed_data = {
        "types": {
            "EIP712Domain": [
                {"name": "name", "type": "string"},
                {"name": "version", "type": "string"},
                {"name": "chainId", "type": "uint256"},
                {"name": "verifyingContract", "type": "address"},
            ],
            "Message": [{"name": "msg", "type": "string"}],
        },
        "primaryType": "Message",
        "domain": {
            "name": "AsterSignTransaction",
            "version": "1",
            "chainId": chain_id,
            "verifyingContract": "0x0000000000000000000000000000000000000000",
        },
        "message": {"msg": msg},
    }

    signable = encode_typed_data(full_message=typed_data)
    signed = Account.sign_message(signable, private_key=private_key)
    print("0x" + signed.signature.hex())
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)
