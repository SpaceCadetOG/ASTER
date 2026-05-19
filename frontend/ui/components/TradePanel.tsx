"use client";

import { useMemo, useState } from "react";
import { formatCompactUsd, formatNumber } from "@/lib/format";
import type { LiveView, ScannerRow, ScannerSide, TokenDetailData } from "@/lib/types";

function liveContext(side: ScannerSide, row: ScannerRow | null, live: LiveView | null) {
  return [
    `${side.toUpperCase()} score ${formatNumber(row?.score, 2)}`,
    `grade ${row?.grade || "N/A"}`,
    `bot mode ${live?.dryRun ? "paper" : live?.liveEnabled ? "live" : "watch-only"}`,
    `available ${formatCompactUsd(live?.availableUsdt)}`
  ].join(" • ");
}

export function TradePanel({
  detail
}: {
  detail: TokenDetailData;
}) {
  const [marginUsd, setMarginUsd] = useState("50");
  const [orderType, setOrderType] = useState<"market" | "limit">("market");
  const [limitPrice, setLimitPrice] = useState(
    detail.scannerRow?.lastPrice ? String(detail.scannerRow.lastPrice) : ""
  );

  const payloadPreview = useMemo(
    () => ({
      symbol: detail.symbol,
      side: detail.requestedSide === "long" ? "BUY" : "SELL",
      marginUsd: Number(marginUsd || 0),
      orderType,
      limitPrice: orderType === "limit" ? Number(limitPrice || 0) : null
    }),
    [detail.symbol, detail.requestedSide, limitPrice, marginUsd, orderType]
  );

  return (
    <section className="panel trade-panel reveal">
      <div className="section-heading">
        <div>
          <span className="eyebrow">Trade Prep</span>
          <h2>Paper / live execution prep</h2>
        </div>
        <span className="status-pill status-warn">backend gap</span>
      </div>

      <p className="panel-copy">
        {liveContext(detail.requestedSide, detail.scannerRow, detail.live)}
      </p>

      <div className="trade-form-grid">
        <label>
          <span>Margin (USD)</span>
          <input
            value={marginUsd}
            onChange={(event) => setMarginUsd(event.target.value)}
            inputMode="decimal"
          />
        </label>

        <label>
          <span>Order Mode</span>
          <select
            value={orderType}
            onChange={(event) => setOrderType(event.target.value as "market" | "limit")}
          >
            <option value="market">Market</option>
            <option value="limit">Limit</option>
          </select>
        </label>

        {orderType === "limit" ? (
          <label>
            <span>Limit Price</span>
            <input
              value={limitPrice}
              onChange={(event) => setLimitPrice(event.target.value)}
              inputMode="decimal"
            />
          </label>
        ) : null}
      </div>

      <div className="alert-card">
        <strong>Execution not wired</strong>
        <p>{detail.tradePanel.reason}</p>
        <p>
          Expected backend contract:{" "}
          <code>
            {detail.tradePanel.expectedContract.method}{" "}
            {detail.tradePanel.expectedContract.path}
          </code>
        </p>
        <p>Required fields: {detail.tradePanel.expectedContract.fields.join(", ")}</p>
      </div>

      <div className="payload-preview">
        <span className="eyebrow">Prepared Payload</span>
        <pre>{JSON.stringify(payloadPreview, null, 2)}</pre>
      </div>
    </section>
  );
}
