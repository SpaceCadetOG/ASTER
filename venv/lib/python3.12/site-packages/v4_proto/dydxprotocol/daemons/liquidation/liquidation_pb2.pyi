from v4_proto.gogoproto import gogo_pb2 as _gogo_pb2
from v4_proto.dydxprotocol.subaccounts import subaccount_pb2 as _subaccount_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class LiquidateSubaccountsRequest(_message.Message):
    __slots__ = ("subaccount_ids",)
    SUBACCOUNT_IDS_FIELD_NUMBER: _ClassVar[int]
    subaccount_ids: _containers.RepeatedCompositeFieldContainer[_subaccount_pb2.SubaccountId]
    def __init__(self, subaccount_ids: _Optional[_Iterable[_Union[_subaccount_pb2.SubaccountId, _Mapping]]] = ...) -> None: ...

class LiquidateSubaccountsResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
