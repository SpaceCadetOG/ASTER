from v4_proto.gogoproto import gogo_pb2 as _gogo_pb2
from v4_proto.dydxprotocol.subaccounts import subaccount_pb2 as _subaccount_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PerpetualLiquidationInfo(_message.Message):
    __slots__ = ("subaccount_id", "perpetual_id")
    SUBACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    PERPETUAL_ID_FIELD_NUMBER: _ClassVar[int]
    subaccount_id: _subaccount_pb2.SubaccountId
    perpetual_id: int
    def __init__(self, subaccount_id: _Optional[_Union[_subaccount_pb2.SubaccountId, _Mapping]] = ..., perpetual_id: _Optional[int] = ...) -> None: ...

class SubaccountLiquidationInfo(_message.Message):
    __slots__ = ("perpetuals_liquidated", "notional_liquidated", "quantums_insurance_lost")
    PERPETUALS_LIQUIDATED_FIELD_NUMBER: _ClassVar[int]
    NOTIONAL_LIQUIDATED_FIELD_NUMBER: _ClassVar[int]
    QUANTUMS_INSURANCE_LOST_FIELD_NUMBER: _ClassVar[int]
    perpetuals_liquidated: _containers.RepeatedScalarFieldContainer[int]
    notional_liquidated: int
    quantums_insurance_lost: int
    def __init__(self, perpetuals_liquidated: _Optional[_Iterable[int]] = ..., notional_liquidated: _Optional[int] = ..., quantums_insurance_lost: _Optional[int] = ...) -> None: ...
