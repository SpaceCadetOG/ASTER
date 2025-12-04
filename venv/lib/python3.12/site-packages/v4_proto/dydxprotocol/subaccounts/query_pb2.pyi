from v4_proto.cosmos_proto import cosmos_pb2 as _cosmos_pb2
from v4_proto.gogoproto import gogo_pb2 as _gogo_pb2
from v4_proto.google.api import annotations_pb2 as _annotations_pb2
from v4_proto.cosmos.base.query.v1beta1 import pagination_pb2 as _pagination_pb2
from v4_proto.dydxprotocol.subaccounts import subaccount_pb2 as _subaccount_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryGetSubaccountRequest(_message.Message):
    __slots__ = ("owner", "number")
    OWNER_FIELD_NUMBER: _ClassVar[int]
    NUMBER_FIELD_NUMBER: _ClassVar[int]
    owner: str
    number: int
    def __init__(self, owner: _Optional[str] = ..., number: _Optional[int] = ...) -> None: ...

class QuerySubaccountResponse(_message.Message):
    __slots__ = ("subaccount",)
    SUBACCOUNT_FIELD_NUMBER: _ClassVar[int]
    subaccount: _subaccount_pb2.Subaccount
    def __init__(self, subaccount: _Optional[_Union[_subaccount_pb2.Subaccount, _Mapping]] = ...) -> None: ...

class QueryAllSubaccountRequest(_message.Message):
    __slots__ = ("pagination",)
    PAGINATION_FIELD_NUMBER: _ClassVar[int]
    pagination: _pagination_pb2.PageRequest
    def __init__(self, pagination: _Optional[_Union[_pagination_pb2.PageRequest, _Mapping]] = ...) -> None: ...

class QuerySubaccountAllResponse(_message.Message):
    __slots__ = ("subaccount", "pagination")
    SUBACCOUNT_FIELD_NUMBER: _ClassVar[int]
    PAGINATION_FIELD_NUMBER: _ClassVar[int]
    subaccount: _containers.RepeatedCompositeFieldContainer[_subaccount_pb2.Subaccount]
    pagination: _pagination_pb2.PageResponse
    def __init__(self, subaccount: _Optional[_Iterable[_Union[_subaccount_pb2.Subaccount, _Mapping]]] = ..., pagination: _Optional[_Union[_pagination_pb2.PageResponse, _Mapping]] = ...) -> None: ...
