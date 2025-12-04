from v4_proto.dydxprotocol.indexer.shared import removal_reason_pb2 as _removal_reason_pb2
from v4_proto.dydxprotocol.indexer.protocol.v1 import clob_pb2 as _clob_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class OrderPlaceV1(_message.Message):
    __slots__ = ("order", "placement_status")
    class OrderPlacementStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ORDER_PLACEMENT_STATUS_UNSPECIFIED: _ClassVar[OrderPlaceV1.OrderPlacementStatus]
        ORDER_PLACEMENT_STATUS_BEST_EFFORT_OPENED: _ClassVar[OrderPlaceV1.OrderPlacementStatus]
        ORDER_PLACEMENT_STATUS_OPENED: _ClassVar[OrderPlaceV1.OrderPlacementStatus]
    ORDER_PLACEMENT_STATUS_UNSPECIFIED: OrderPlaceV1.OrderPlacementStatus
    ORDER_PLACEMENT_STATUS_BEST_EFFORT_OPENED: OrderPlaceV1.OrderPlacementStatus
    ORDER_PLACEMENT_STATUS_OPENED: OrderPlaceV1.OrderPlacementStatus
    ORDER_FIELD_NUMBER: _ClassVar[int]
    PLACEMENT_STATUS_FIELD_NUMBER: _ClassVar[int]
    order: _clob_pb2.IndexerOrder
    placement_status: OrderPlaceV1.OrderPlacementStatus
    def __init__(self, order: _Optional[_Union[_clob_pb2.IndexerOrder, _Mapping]] = ..., placement_status: _Optional[_Union[OrderPlaceV1.OrderPlacementStatus, str]] = ...) -> None: ...

class OrderRemoveV1(_message.Message):
    __slots__ = ("removed_order_id", "reason", "removal_status")
    class OrderRemovalStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ORDER_REMOVAL_STATUS_UNSPECIFIED: _ClassVar[OrderRemoveV1.OrderRemovalStatus]
        ORDER_REMOVAL_STATUS_BEST_EFFORT_CANCELED: _ClassVar[OrderRemoveV1.OrderRemovalStatus]
        ORDER_REMOVAL_STATUS_CANCELED: _ClassVar[OrderRemoveV1.OrderRemovalStatus]
        ORDER_REMOVAL_STATUS_FILLED: _ClassVar[OrderRemoveV1.OrderRemovalStatus]
    ORDER_REMOVAL_STATUS_UNSPECIFIED: OrderRemoveV1.OrderRemovalStatus
    ORDER_REMOVAL_STATUS_BEST_EFFORT_CANCELED: OrderRemoveV1.OrderRemovalStatus
    ORDER_REMOVAL_STATUS_CANCELED: OrderRemoveV1.OrderRemovalStatus
    ORDER_REMOVAL_STATUS_FILLED: OrderRemoveV1.OrderRemovalStatus
    REMOVED_ORDER_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    REMOVAL_STATUS_FIELD_NUMBER: _ClassVar[int]
    removed_order_id: _clob_pb2.IndexerOrderId
    reason: _removal_reason_pb2.OrderRemovalReason
    removal_status: OrderRemoveV1.OrderRemovalStatus
    def __init__(self, removed_order_id: _Optional[_Union[_clob_pb2.IndexerOrderId, _Mapping]] = ..., reason: _Optional[_Union[_removal_reason_pb2.OrderRemovalReason, str]] = ..., removal_status: _Optional[_Union[OrderRemoveV1.OrderRemovalStatus, str]] = ...) -> None: ...

class OrderUpdateV1(_message.Message):
    __slots__ = ("order_id", "total_filled_quantums")
    ORDER_ID_FIELD_NUMBER: _ClassVar[int]
    TOTAL_FILLED_QUANTUMS_FIELD_NUMBER: _ClassVar[int]
    order_id: _clob_pb2.IndexerOrderId
    total_filled_quantums: int
    def __init__(self, order_id: _Optional[_Union[_clob_pb2.IndexerOrderId, _Mapping]] = ..., total_filled_quantums: _Optional[int] = ...) -> None: ...

class OffChainUpdateV1(_message.Message):
    __slots__ = ("order_place", "order_remove", "order_update")
    ORDER_PLACE_FIELD_NUMBER: _ClassVar[int]
    ORDER_REMOVE_FIELD_NUMBER: _ClassVar[int]
    ORDER_UPDATE_FIELD_NUMBER: _ClassVar[int]
    order_place: OrderPlaceV1
    order_remove: OrderRemoveV1
    order_update: OrderUpdateV1
    def __init__(self, order_place: _Optional[_Union[OrderPlaceV1, _Mapping]] = ..., order_remove: _Optional[_Union[OrderRemoveV1, _Mapping]] = ..., order_update: _Optional[_Union[OrderUpdateV1, _Mapping]] = ...) -> None: ...
