# Phase 11: Cloud Run Frontend / Dashboard

## Objective

Deploy the existing `ui/scanner-dashboard` Next.js application as a read-only frontend on Cloud Run.

This phase prepares the frontend image build and deployment path only.

Guardrails:

- read-only dashboard only
- no trade execution
- no secret exposure
- no live trading
- no official paper baseline run
- no changes to `cmd/live`, scanner/ranking logic, or paper-auto behavior

## Current Architecture

Current backend APIs are Go services, not Python services.

Management VM APIs:

- long: `:8080`
- short: `:8081`
- oflow: `:8090`
- tape: `:8091`
- whale: `:8092`
- liqs: `:8093`

Execution VM API:

- live: `:8787`

For this first Cloud Run deploy, private backend connectivity is optional and should not be required.

The safest initial deployment is:

- Cloud Run service deployed in read-only mode
- `SCANNER_USE_MOCK=true`

That produces a public dashboard shell without introducing VPC connector work or private API exposure in the same phase.

## Artifact Registry

- project: `aster-tradingbot`
- region: `us-south1`
- repo: `aster-apps`
- image: `scanner-dashboard`

## Docker Image Build

Cloud Build config:

- [cloudbuild/frontend-dashboard.yaml](/Users/victorogbebor/2026/go-machine/cloudbuild/frontend-dashboard.yaml:1)

Manual build command from repo root:

```bash
gcloud builds submit \
  --config cloudbuild/frontend-dashboard.yaml \
  .
```

This builds and pushes:

- `us-south1-docker.pkg.dev/$PROJECT_ID/aster-apps/scanner-dashboard:$SHORT_SHA`
- `us-south1-docker.pkg.dev/$PROJECT_ID/aster-apps/scanner-dashboard:latest`

## Cloud Run Deploy

Initial safe deploy:

```bash
gcloud run deploy scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --platform=managed \
  --image=us-south1-docker.pkg.dev/aster-tradingbot/aster-apps/scanner-dashboard:latest \
  --allow-unauthenticated \
  --port=8080 \
  --set-env-vars=SCANNER_USE_MOCK=true
```

Notes:

- `SCANNER_USE_MOCK=true` avoids requiring private connectivity for the first release
- no secrets are needed for this deploy
- do not pass ASTER auth config to Cloud Run

## Optional Future Backend Wiring

Later, when private backend access is intentionally added, the frontend can be configured with optional env vars such as:

```bash
SCANNER_LONG_URL=http://10.10.10.2:8080
SCANNER_SHORT_URL=http://10.10.10.2:8081
SCANNER_LIVE_URL=http://10.20.10.2:8787
SCANNER_OFLOW_URL=http://10.10.10.2:8090
SCANNER_TAPE_URL=http://10.10.10.2:8091
SCANNER_WHALE_URL=http://10.10.10.2:8092
SCANNER_LIQS_URL=http://10.10.10.2:8093
```

That private wiring is a later sub-step and is out of scope for this phase.

## Validation Commands

Check the deployed service:

```bash
gcloud run services describe scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1
```

Fetch the service URL:

```bash
gcloud run services describe scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --format='value(status.url)'
```

Open the service:

```bash
curl -I "$(gcloud run services describe scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --format='value(status.url)')"
```

## Rollback

List revisions:

```bash
gcloud run revisions list \
  --project=aster-tradingbot \
  --region=us-south1 \
  --service=scanner-dashboard
```

Shift traffic back to a prior good revision:

```bash
gcloud run services update-traffic scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --to-revisions=REVISION_NAME=100
```

## Guardrails

- Do not add trade execution in Phase 11
- Do not expose `/etc/aster/.aster.yaml`
- Do not inject Secret Manager runtime secrets into the frontend
- Do not enable live trading
- Do not start the official paper validation baseline
- Do not change scanner/ranking semantics

## Local Validation

From the dashboard directory:

```bash
cd /Users/victorogbebor/2026/go-machine/ui/scanner-dashboard
npm install
npm run build
```

Optional local Docker build:

```bash
docker build -f ui/scanner-dashboard/Dockerfile ui/scanner-dashboard
```
