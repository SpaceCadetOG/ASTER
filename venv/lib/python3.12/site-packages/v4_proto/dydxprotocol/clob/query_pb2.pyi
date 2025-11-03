from v4_proto.gogoproto import gogo_pb2 as _gogo_pb2
from v4_proto.google.api import annotations_pb2 as _annotations_pb2
from v4_proto.cosmos.base.query.v1beta1 import pagination_pb2 as _pagination_pb2
from v4_proto.dydxprotocol.clob import block_rate_limit_config_pb2 as _block_rate_limit_config_pb2
from v4_proto.dydxprotocol.clob import clob_pair_pb2 as _clob_pair_pb2
from v4_proto.dydxprotocol.clob import equity_tier_limit_config_pb2 as _equity_tier_limit_config_pb2
from v4_proto.dydxprotocol.clob import liquidations_config_pb2 as _liquidations_config_pb2
from v4_proto.dydxprotocol.clob import mev_pb2 as _mev_pb2
from v4_proto.dydxprotocol.subaccounts import subaccount_pb2 as _subaccount_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryGetClobPairRequest(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: int
    def __init__(self, id: _Optional[int] = ...) -> None: ...

class QueryClobPairResponse(_message.Message):
    __slots__ = ("clob_pair",)
    CLOB_PAIR_FIELD_NUMBER: _ClassVar[int]
    clob_pair: _clob_pair_pb2.ClobPair
    def __init__(self, clob_pair: _Optional[_Union[_clob_pair_pb2.ClobPair, _Mapping]] = ...) -> None: ...

class QueryAllClobPairRequest(_message.Message):
    __slots__ = ("pagination",)
    PAGINATION_FIELD_NUMBER: _ClassVar[int]
    pagination: _pagination_pb2.PageRequest
    def __init__(self, pagination: _Optional[_Union[_pagination_pb2.PageRequest, _Mapping]] = ...) -> None: ...

class QueryClobPairAllResponse(_message.Message):
    __slots__ = ("clob_pair", "pagination")
    CLOB_PAIR_FIELD_NUMBER: _ClassVar[int]
    PAGINATION_FIELD_NUMBER: _ClassVar[int]
    clob_pair: _containers.RepeatedCompositeFieldContainer[_clob_pair_pb2.ClobPair]
    pagination: _pagination_pb2.PageResponse
    def __init__(self, clob_pair: _Optional[_Iterable[_Union[_clob_pair_pb2.ClobPair, _Mapping]]] = ..., pagination: _Optional[_Union[_pagination_pb2.PageResponse, _Mapping]] = ...) -> None: ...

class AreSubaccountsLiquidatableRequest(_message.Message):
    __slots__ = ("subaccount_ids",)
    SUBACCOUNT_IDS_FIELD_NUMBER: _ClassVar[int]
    subaccount_ids: _containers.RepeatedCompositeFieldContainer[_subaccount_pb2.SubaccountId]
    def __init__(self, subaccount_ids: _Optional[_Iterable[_Union[_subaccount_pb2.SubaccountId, _Mapping]]] = ...) -> None: ...

class AreSubaccountsLiquidatableResponse(_message.Message):
    __slots__ = ("results",)
    class Result(_message.Message):
        __slots__ = ("subaccount_id", "is_liquidatable")
        SUBACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
        IS_LIQUIDATABLE_FIELD_NUMBER: _ClassVar[int]
        subaccount_id: _subaccount_pb2.SubaccountId
        is_liquidatable: bool
        def __init__(self, subaccount_id: _Optional[_Union[_subaccount_pb2.SubaccountId, _Mapping]] = ..., is_liquidatable: bool = ...) -> None: ...
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    results: _containers.RepeatedCompositeFieldContainer[AreSubaccountsLiquidatableResponse.Result]
    def __init__(self, results: _Optional[_Iterable[_Union[AreSubaccountsLiquidatableResponse.Result, _Mapping]]] = ...) -> None: ...

class MevNodeToNodeCalculationRequest(_message.Message):
    __slots__ = ("block_proposer_matches", "validator_mev_metrics")
    BLOCK_PROPOSER_MATCHES_FIELD_NUMBER: _ClassVar[int]
    VALIDATOR_MEV_METRICS_FIELD_NUMBER: _ClassVar[int]
    block_proposer_matches: _mev_pb2.ValidatorMevMatches
    validator_mev_metrics: _mev_pb2.MevNodeToNodeMetrics
    def __init__(self, block_proposer_matches: _Optional[_Union[_mev_pb2.ValidatorMevMatches, _Mapping]] = ..., validator_mev_metrics: _Optional[_Union[_mev_pb2.MevNodeToNodeMetrics, _Mapping]] = ...) -> None: ...

class MevNodeToNodeCalculationResponse(_message.Message):
    __slots__ = ("results",)
    class MevAndVolumePerClob(_message.Message):
        __slots__ = ("clob_pair_id", "mev", "volume")
        CLOB_PAIR_ID_FIELD_NUMBER: _ClassVar[int]
        MEV_FIELD_NUMBER: _ClassVar[int]
        VOLUME_FIELD_NUMBER: _ClassVar[int]
        clob_pair_id: int
        mev: float
        volume: int
        def __init__(self, clob_pair_id: _Optional[int] = ..., mev: _Optional[float] = ..., volume: _Optional[int] = ...) -> None: ...
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    results: _containers.RepeatedCompositeFieldContainer[MevNodeToNodeCalculationResponse.MevAndVolumePerClob]
    def __init__(self, results: _Optional[_Iterable[_Union[MevNodeToNodeCalculationResponse.MevAndVolumePerClob, _Mapping]]] = ...) -> None: ...

class QueryEquityTierLimitConfigurationRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class QueryEquityTierLimitConfigurationResponse(_message.Message):
    __slots__ = ("equity_tier_limit_config",)
    EQUITY_TIER_LIMIT_CONFIG_FIELD_NUMBER: _ClassVar[int]
    equity_tier_limit_config: _equity_tier_limit_config_pb2.EquityTierLimitConfiguration
    def __init__(self, equity_tier_limit_config: _Optional[_Union[_equity_tier_limit_config_pb2.EquityTierLimitConfiguration, _Mapping]] = ...) -> None: ...

class QueryBlockRateLimitConfigurationRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class QueryBlockRateLimitConfigurationResponse(_message.Message):
    __slots__ = ("block_rate_limit_config",)
    BLOCK_RATE_LIMIT_CONFIG_FIELD_NUMBER: _ClassVar[int]
    block_rate_limit_config: _block_rate_limit_config_pb2.BlockRateLimitConfiguration
    def __init__(self, block_rate_limit_config: _Optional[_Union[_block_rate_limit_config_pb2.BlockRateLimitConfiguration, _Mapping]] = ...) -> None: ...

class QueryLiquidationsConfigurationRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class QueryLiquidationsConfigurationResponse(_message.Message):
    __slots__ = ("liquidations_config",)
    LIQUIDATIONS_CONFIG_FIELD_NUMBER: _ClassVar[int]
    liquidations_config: _liquidations_config_pb2.LiquidationsConfig
    def __init__(self, liquidations_config: _Optional[_Union[_liquidations_config_pb2.LiquidationsConfig, _Mapping]] = ...) -> None: ...
