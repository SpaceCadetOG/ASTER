# Phase 12: Frontend Cloud Run CI/CD Auto Deploy

## Objective

Automatically build and deploy the unified ASTER frontend to Cloud Run whenever code is pushed or merged to `main`.

This phase applies only to the frontend deployment path:

- source path: `ui/scanner-dashboard`
- Cloud Run service: `scanner-dashboard`
- region: `us-south1`
- Artifact Registry repo: `aster-apps`

Guardrails:

- Cloud Run frontend remains read-only
- no mock mode in Cloud Run
- no changes to `cmd/live`
- no scanner/ranking logic changes
- no paper-auto changes
- no trade execution controls
- no secrets committed into the repo

## Deployment Model

Cloud Build should be connected to the GitHub repo and triggered on pushes to `main`:

- repo: `SpaceCadetOG/ASTER`
- branch: `main`
- config: `cloudbuild/frontend-dashboard.yaml`

On each `main` push, Cloud Build will:

1. build `ui/scanner-dashboard/Dockerfile` using `ui/scanner-dashboard` as the build context
2. tag the image with `$SHORT_SHA`
3. tag the image with `latest`
4. push both tags to Artifact Registry
5. deploy the `$SHORT_SHA` image to the existing Cloud Run service `scanner-dashboard`

## Cloud Build Config

Config file:

- [cloudbuild/frontend-dashboard.yaml](/Users/victorogbebor/2026/go-machine/cloudbuild/frontend-dashboard.yaml)

Image targets:

- `us-south1-docker.pkg.dev/$PROJECT_ID/aster-apps/scanner-dashboard:$SHORT_SHA`
- `us-south1-docker.pkg.dev/$PROJECT_ID/aster-apps/scanner-dashboard:latest`

## Cloud Run Runtime Settings

The Cloud Run frontend must keep:

- service: `scanner-dashboard`
- region: `us-south1`
- VPC connector: `scanner-dashboard-conn`
- VPC egress: `private-ranges-only`

Cloud Run must not set `SCANNER_USE_MOCK`.

## Backend Environment Variables

The frontend is wired to private backend APIs through environment variables:

```bash
SCANNER_LONG_URL=http://10.10.10.2:8080
SCANNER_SHORT_URL=http://10.10.10.2:8081
SCANNER_LIVE_URL=http://10.20.10.2:8787
SCANNER_OFLOW_URL=http://10.10.10.2:8090
SCANNER_TAPE_URL=http://10.10.10.2:8091
SCANNER_WHALE_URL=http://10.10.10.2:8092
SCANNER_LIQS_URL=http://10.10.10.2:8093
```

These are private internal service addresses, not credentials. No auth secrets are committed in this phase.

## Trigger Setup

Recommended Cloud Build trigger configuration:

- event: push to branch
- branch: `^main$`
- source: GitHub repo `SpaceCadetOG/ASTER`
- config file: `cloudbuild/frontend-dashboard.yaml`

## Validation

### Local frontend build with Node 20

```bash
cd /Users/victorogbebor/2026/go-machine/ui/scanner-dashboard
/Users/victorogbebor/.nvm/versions/node/v20.10.0/bin/node \
  /Users/victorogbebor/.nvm/versions/node/v20.10.0/lib/node_modules/npm/bin/npm-cli.js \
  run build
```

### Cloud Build config syntax

```bash
ruby -e 'require "yaml"; YAML.load_file("cloudbuild/frontend-dashboard.yaml"); puts "cloudbuild yaml ok"'
```

### Manual Cloud Build submit

```bash
gcloud builds submit \
  --project=aster-tradingbot \
  --config=cloudbuild/frontend-dashboard.yaml \
  .
```

## Rollback

List recent revisions:

```bash
gcloud run revisions list \
  --project=aster-tradingbot \
  --region=us-south1 \
  --service=scanner-dashboard
```

Shift traffic to a previous good revision:

```bash
gcloud run services update-traffic scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --to-revisions=REVISION_NAME=100
```

Manual redeploy by image tag if needed:

```bash
gcloud run deploy scanner-dashboard \
  --project=aster-tradingbot \
  --region=us-south1 \
  --platform=managed \
  --image=us-south1-docker.pkg.dev/aster-tradingbot/aster-apps/scanner-dashboard:latest \
  --allow-unauthenticated \
  --port=8080 \
  --vpc-connector=scanner-dashboard-conn \
  --vpc-egress=private-ranges-only \
  --set-env-vars=SCANNER_LONG_URL=http://10.10.10.2:8080,SCANNER_SHORT_URL=http://10.10.10.2:8081,SCANNER_LIVE_URL=http://10.20.10.2:8787,SCANNER_OFLOW_URL=http://10.10.10.2:8090,SCANNER_TAPE_URL=http://10.10.10.2:8091,SCANNER_WHALE_URL=http://10.10.10.2:8092,SCANNER_LIQS_URL=http://10.10.10.2:8093
```

## Non-Goals

This phase does not modify:

- `cmd/live`
- scanner ranking behavior
- paper-auto logic
- execution or trading logic
- VM runtime services
- secrets management
