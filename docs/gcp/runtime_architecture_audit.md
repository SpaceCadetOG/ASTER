# ASTER GCP Runtime Architecture Audit

Date: 2026-05-19

## 1. Executive Summary

The current ASTER codebase does **not** treat the standalone scanner and flow commands as required upstream dependencies of `cmd/live`.

Today, `cmd/live` is a **self-contained execution runtime** that:

- fetches markets directly from the exchange client
- computes its own long/short scanner rankings internally
- builds and maintains its own in-play tracker state
- builds its own watch set and internal flow metrics
- runs its own paper/live trade management loop
- exposes its own `/healthz` and `/api/status` runtime surface

By contrast, `cmd/long`, `cmd/short`, `cmd/tape`, `cmd/whale`, `cmd/liqs`, and `cmd/oflow` are best understood as **standalone operator-facing scanner/analysis services** and **dashboard/API sources**, not as required runtime prerequisites for `cmd/live`.

The biggest architecture finding is that the current dependency direction is closer to:

- `cmd/live` -> self-contained scanner/execution/status
- some sidecars -> optionally seed themselves from `cmd/live` status

not:

- `cmd/live` -> remote long/short scanner APIs

That means the current Taiwan execution VM does **not** require the Texas management VM in order to operate in paper mode or live mode. The paper runtime behavior you observed in GCP is consistent with the code and should be considered healthy.

The best near-term cloud architecture is therefore a **hybrid/fallback-preserving model**:

- keep `cmd/live` self-contained on the execution VM
- keep management-plane services in the US for operator visibility, dashboard/API access, and optional analytics
- avoid introducing a hard cross-region dependency from execution to management unless there is a strong later reason to do so

## 2. What We Deployed In GCP So Far

Completed cloud foundation and runtime steps:

- `aster-tradingbot` project
- `aster-vpc` custom VPC with global dynamic routing
- US management subnet: `10.10.10.0/24`
- US frontend subnet: `10.10.20.0/24`
- Taiwan execution subnet: `10.20.10.0/24`
- Cloud Routers and Cloud NAT in both regions
- firewall rules for private scanner/status traffic
- runtime service accounts for management, execution, frontend, and Terraform
- private-only management VM at `10.10.10.2`
- private-only execution VM at `10.20.10.2`
- management binaries:
  - `long`
  - `short`
  - `tape`
  - `whale`
  - `liqs`
  - `oflow`
- execution binaries:
  - `live`
  - `exec`

Runtime validation completed:

- management long/short scanners are reachable privately across regions
- all management sidecars were brought up and emitting live stream output
- execution `cmd/live` started in paper mode and produced a populated status surface without being pointed at `10.10.10.2:8080` or `:8081`

## 3. Current Repo Runtime Architecture

### Core architectural split

There are effectively three runtime classes in the repo:

1. `cmd/live`
- execution runtime
- self-contained scanner/ranking loop
- paper/live trade management
- own status API

2. standalone scanner surfaces
- `cmd/long`
- `cmd/short`
- each is a mini web app plus JSON API

3. standalone flow/market-microstructure sidecars
- `cmd/tape`
- `cmd/whale`
- `cmd/liqs`
- `cmd/oflow`
- each exposes health/status/asset analysis APIs

### Evidence in the repo

`cmd/live` is described and used as a standalone runtime in:

- [scripts/run_live_logged.sh](/Users/victorogbebor/2026/go-machine/scripts/run_live_logged.sh:1)
- [docs/gitbook/guides/quickstart.md](/Users/victorogbebor/2026/go-machine/docs/gitbook/guides/quickstart.md:13)
- [docs/live_env_defaults.md](/Users/victorogbebor/2026/go-machine/docs/live_env_defaults.md:19)

The quickstart explicitly allows:

- `go run ./cmd/live`

without requiring `cmd/long` or `cmd/short` to be running first.

## 4. `cmd/live` Scanner/Ranking Data Path

### How ranked scanner state is obtained today

`cmd/live` instantiates its own scanner pipeline directly inside the runtime loop.

In [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:1882), the scanner worker:

- fetches all markets from the exchange client
- computes long rows with `market.ScoreAndFilter(mkts)`
- computes short rows with `market.ScoreAndFilterShort(mkts)`
- optionally narrows the universe using `internal/discovery`
- builds symbol metadata
- computes eligible long and short candidates
- updates internal long and short in-play trackers
- publishes an in-memory `liveScannerSnapshot`

That snapshot then feeds the rest of the runtime through [cmd/live/live_runtime_loop.go](/Users/victorogbebor/2026/go-machine/cmd/live/live_runtime_loop.go:1), where the loop stores the latest scanner and watcher snapshots in memory.

### Shared packages used by `cmd/live`

`cmd/live` does reuse shared internal packages, but it does so **locally**, not through remote process calls:

- `internal/discovery` for universe narrowing
- `internal/gate` for entry gating
- `internal/inplay` for tracking in-play candidate state
- market scoring/confluence helpers inside the repo used directly by the live runtime

### Does `cmd/live` ever call external long/short HTTP APIs?

No evidence was found that `cmd/live` calls `cmd/long` or `cmd/short` over HTTP.

Searches for scanner API ports and URL envs found:

- `SHORT_SCANNER_URL` and `LONG_SCANNER_URL` only in `cmd/long` and `cmd/short` for UI linking
- no `cmd/live` HTTP client code targeting `8080` or `8081`
- no `cmd/live` env knob that points to a long/short API

### Does `cmd/live` read local scanner artifacts or files?

Not for primary scanner ranking.

The only file-based external signal path identified is:

- `LIVE_FLOW_FEED_FILE`

In [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:1678), `cmd/live` reads `LIVE_FLOW_FEED_FILE` and constructs a file feed:

- `externalFlowFeed := flowfeed.NewFileFeed(flowFeedPath, flowFeedTTL)`

That feed is then used as an **advisory external flow/liquidation signal source**, especially around momentum exit logic, not as the core scanner source.

Relevant code:

- [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:1878)
- [internal/flow/feed.go](/Users/victorogbebor/2026/go-machine/internal/flow/feed.go:1)

### Why Phase 7 showed ranked scanner output in Taiwan

That behavior is expected.

Once the self-contained scanner worker runs, the decision loop enters a manual-only scanner mode:

- `manualOnlyScannerMode := true`

and populates status fields such as:

- `scanner_longs`
- `scanner_shorts`
- `top_symbol`
- `top_side`
- `top_decision`

Then it logs:

- `live: scanner-only top ...`

Relevant code:

- [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:2495)
- [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:2535)

This fully explains the observed Phase 7 runtime output without any dependency on the management VM.

## 5. Relationship Of `live` ↔ `long` / `short` / `tape` / `whale` / `liqs` / `oflow`

### `cmd/long`

Current role:

- standalone long-side scanner UI/API
- operator/reporting tool
- dashboard/API source

Evidence:

- serves UI and JSON endpoints directly from [cmd/long/main.go](/Users/victorogbebor/2026/go-machine/cmd/long/main.go:1)
- computes its own long-side market scan

Classification:

- operator/reporting tool
- dashboard/API source
- legacy-but-still-useful standalone scanner

It is **not** a required runtime dependency of `cmd/live` today.

### `cmd/short`

Current role:

- standalone short-side scanner UI/API
- operator/reporting tool
- dashboard/API source

Evidence:

- serves UI and JSON endpoints directly from [cmd/short/main.go](/Users/victorogbebor/2026/go-machine/cmd/short/main.go:1)
- computes its own short-side market scan

Classification:

- operator/reporting tool
- dashboard/API source
- legacy-but-still-useful standalone scanner

It is **not** a required runtime dependency of `cmd/live` today.

### `cmd/tape`

Current role:

- standalone tape/print analysis sidecar
- dashboard/API source
- optional market-microstructure tool

Evidence:

- exposes `/healthz`, `/api/status`, and `/api/asset` in [cmd/tape/main.go](/Users/victorogbebor/2026/go-machine/cmd/tape/main.go:1)
- defaults to seeding symbols from `cmd/live` status through `TAPE_LIVE_STATUS_URL`

Classification:

- optional sidecar
- dashboard/API source
- operator analysis tool

It is **not** consumed by `cmd/live` today.

### `cmd/whale`

Current role:

- standalone whale-detection sidecar
- dashboard/API source
- optional flow/large-trade analysis tool

Evidence:

- exposes `/healthz`, `/api/status`, and `/api/asset` in [cmd/whale/main.go](/Users/victorogbebor/2026/go-machine/cmd/whale/main.go:1)
- defaults to seeding symbols from `cmd/live` status through `WHALE_LIVE_STATUS_URL`

Classification:

- optional sidecar
- dashboard/API source
- operator analysis tool

It is **not** consumed by `cmd/live` today.

### `cmd/liqs`

Current role:

- standalone liquidation-stream analysis sidecar
- dashboard/API source

Evidence:

- exposes `/healthz`, `/api/status`, and `/api/asset` in [cmd/liqs/main.go](/Users/victorogbebor/2026/go-machine/cmd/liqs/main.go:1)
- subscribes to liquidation stream independently

Classification:

- optional sidecar
- dashboard/API source
- operator analysis tool

It is **not** a required dependency of `cmd/live` today.

### `cmd/oflow`

Current role:

- standalone order-flow analysis sidecar
- dashboard/API source

Evidence:

- exposes `/healthz`, `/api/status`, and `/api/asset` in [cmd/oflow/main.go](/Users/victorogbebor/2026/go-machine/cmd/oflow/main.go:1)
- defaults to seeding symbols from `cmd/live` status through `OFLOW_LIVE_STATUS_URL`

Classification:

- optional sidecar
- dashboard/API source
- operator analysis tool

It is **not** consumed by `cmd/live` today.

### Key inversion to note

The sidecars are closer to being **followers of `cmd/live`’s symbol universe** than producers for `cmd/live`.

That logic lives in:

- [internal/scanneruniverse/scanneruniverse.go](/Users/victorogbebor/2026/go-machine/internal/scanneruniverse/scanneruniverse.go:1)

where `ResolveCSVOrScanner(...)` can fetch symbols from status URLs containing:

- `Rows`
- `InPlay`
- `scanner_longs`
- `scanner_shorts`

This is the opposite of the original infrastructure assumption that `cmd/live` might need to consume the long/short scanner services.

## 6. What The Management VM Is Actually Doing Today

Today the management VM is providing:

- independent long scanner UI/API
- independent short scanner UI/API
- standalone flow/analysis sidecars
- rich operator-facing dashboard/API surfaces for the future frontend
- cross-check and observability value for what the market looks like from a US control-plane context

It is **not currently the canonical scanner source for `cmd/live`**.

In practical terms, the management VM is acting as:

- operator/reporting plane
- dashboard/API plane
- optional analytics plane

not:

- required execution intelligence plane

## 7. What The Execution VM Is Actually Doing Today

Today the execution VM is running a self-contained paper/live runtime that:

- fetches markets directly from the exchange
- computes long/short scanner rankings internally
- maintains in-play candidate state
- builds watch symbols from scanner state and open/pending positions
- computes or refreshes local flow metrics through the internal watcher
- publishes status at `/healthz` and `/api/status`
- runs paper accounting when `LIVE_DRY_RUN=true`

This is the canonical runtime for:

- top scanner candidate selection
- paper/live decision state
- execution state
- runtime status for execution

## 8. Does `live` Require The Management Plane?

### Short answer

No.

### More precise answer

`cmd/live` does **not** currently require the Texas management VM in order to:

- scan markets
- rank long/short opportunities
- produce `scanner_longs` / `scanner_shorts`
- decide the top symbol/side
- run paper mode
- expose health/status

### Partial / optional linkage that does exist

There are only optional or indirect linkages today:

1. file-based external flow feed
- `LIVE_FLOW_FEED_FILE`
- local file path, not the management VM APIs

2. sidecar symbol seeding
- some sidecars can consume `cmd/live` status URLs
- this is the reverse dependency direction

3. human/operator workflows
- management-plane scanners and flow APIs are useful for visibility and frontend drilldown
- but not required for `cmd/live` runtime health

## 9. Option A vs B vs C Comparison

### Option A

- keep `cmd/live` self-contained in Taiwan
- keep US management VM for scanners, dashboard, observability, operator visibility

Pros:

- matches current codebase reality
- lowest latency for execution-critical scanner/ranking loop
- avoids cross-region dependency for core execution
- resilient if US management plane goes down
- simplest runtime model

Cons:

- duplicated scanning work exists across live and standalone long/short
- management scanner outputs are informative, not canonical for execution

### Option B

- refactor `cmd/live` so execution consumes management-plane scanner APIs over the private VPC
- US management becomes canonical scanner/ranking source
- Taiwan execution becomes thinner

Pros:

- removes duplicated scanner computation
- makes management plane the single scanner source of truth
- can simplify public/dashboard alignment

Cons:

- directly conflicts with current self-contained architecture
- adds cross-region latency and a new failure domain
- makes execution correctness depend on remote HTTP surfaces
- increases operational complexity before there is clear product need
- riskiest option for live trading logic

### Option C

- hybrid model
- `cmd/live` remains self-contained for safety/fallback
- execution may optionally ingest or cross-check management-plane APIs

Pros:

- preserves current safe architecture
- allows experimentation with external advisory signals
- can reduce organizational friction between frontend/dashboard and execution over time
- easier future migration path if scanner canonization becomes desirable later

Cons:

- still leaves some duplication in place
- adds design complexity if pursued too early

## 10. Recommended Architecture Choice

Recommended option: **Option C**, with an immediate operating posture that looks mostly like Option A.

### Why

Based on the current codebase:

- `cmd/live` is already self-contained
- standalone scanner/flow services are not required runtime prerequisites
- latency-sensitive execution is better protected if scanning/ranking stays local to the execution VM
- the management plane is still highly valuable for UI, observability, and operator workflow

### Practical recommendation

Use this architecture now:

- Taiwan execution VM:
  - canonical `cmd/live` scanner/ranking/execution runtime
- US management VM:
  - independent scanner UIs
  - tape/whale/liqs/oflow APIs
  - future dashboard data plane
  - operator visibility and drilldown plane

Only consider management-to-live scanner ingestion later as an **optional advisory feature**, not as the primary execution path.

## 11. Smallest Safe Patch Path If A Change Is Recommended

No immediate code patch is required to make the current GCP deployment architecture valid.

If a later refactor is desired, the smallest safe path would be:

### Goal

Allow `cmd/live` to optionally ingest remote management-plane scanner snapshots for comparison or advisory enrichment, while keeping the existing self-contained scanner as the primary path.

### Recommended mode

- optional feature flag
- advisory-only external feed
- not required primary mode

### Likely files and areas to change

- [cmd/live/main.go](/Users/victorogbebor/2026/go-machine/cmd/live/main.go:1882)
  - scanner worker
  - candidate ranking path
  - status annotation path
- [cmd/live/live_runtime_loop.go](/Users/victorogbebor/2026/go-machine/cmd/live/live_runtime_loop.go:1)
  - if a remote snapshot needs to be stored alongside local scanner state
- new helper package, likely under `internal/`
  - for a typed scanner status client
- documentation/env references:
  - [systemd/env/live.env.example](/Users/victorogbebor/2026/go-machine/systemd/env/live.env.example:1)
  - [docs/live_env_defaults.md](/Users/victorogbebor/2026/go-machine/docs/live_env_defaults.md:1)

### Safe feature concept

Examples of future env knobs:

- `LIVE_REMOTE_SCANNER_STATUS_URLS`
- `LIVE_REMOTE_SCANNER_MODE=off|advisory|crosscheck`

### Risks to live trading logic

- stale remote snapshots
- cross-region network delay
- divergence between local and remote rankings
- making live runtime behavior less deterministic
- accidental promotion of an advisory feed into a hard dependency

Because of those risks, a required-primary-mode refactor is **not** recommended at this stage.

## 12. Recommended Next GCP Phase

The next GCP phase should **not** be “connect live to management feed.”

That would optimize the wrong layer first.

### Recommended next phase

**Runtime hardening and cloud-native operationalization**

Priority order:

1. persistent service management for management and execution runtimes
- systemd or containerized service supervision
- clean restart behavior
- ordered startup
- host reboot recovery

2. Secret Manager integration for real execution credentials
- exchange auth
- Telegram secrets
- runtime secrets

3. Cloud Run frontend rollout
- public UI
- private calls into management-plane APIs
- optionally read-only status from execution plane

4. logging and artifact strategy
- Cloud Logging already started via Ops Agent
- next add durable log/archive policy if needed

5. CI/CD hardening
- Artifact Registry
- Cloud Build

### Why this is the correct next step

The repo architecture findings show:

- execution already works without management-plane scanner dependency
- the current gap is operational maturity, not missing scanner linkage
- frontend/public surface and runtime supervision now matter more than scanner refactoring

## 13. Risks / Caveats

1. Scanner duplication is real
- `cmd/live` and `cmd/long`/`cmd/short` each compute scanner/ranking state
- this is redundant work, but it currently buys fault isolation

2. Sidecar dependency direction may surprise operators
- `tape`, `whale`, and `oflow` can default to `cmd/live` status for universe seeding
- if operators assume the management plane is upstream of live, they may misread the actual runtime graph

3. File-fed external flow remains a special-case integration
- `LIVE_FLOW_FEED_FILE` is real
- it should be treated as advisory and monitored carefully if enabled in cloud

4. Frontend architecture should respect current backend reality
- the future public frontend should treat management-plane services as read-only scanner/analysis APIs
- it should not assume that those APIs currently drive execution

5. Refactoring too early would increase risk
- moving execution onto remote scanner APIs now would add fragility without clear benefit

## Terminal Summary

- live data path: self-contained market fetch -> internal scoring -> discovery/gating -> in-play tracker -> watcher/flow metrics -> candidate ranking -> paper/live loop
- management modules consumed by live: none as required runtime inputs; only optional local file feed exists via `LIVE_FLOW_FEED_FILE`
- recommended cloud architecture: keep `cmd/live` self-contained on execution VM, keep management VM for scanner/flow/dashboard/operator surfaces, add only optional advisory integration later
- next GCP phase: runtime hardening with persistent services plus Secret Manager, then Cloud Run frontend
- code patch recommended now? no
