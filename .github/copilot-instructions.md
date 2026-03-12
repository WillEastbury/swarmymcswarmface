# Copilot Instructions

## Project overview

SwarmyMcSwarmFace is an agentic SDLC toolkit with three components:

1. **Issue Worker** (`src/issue-worker.js`) — Node.js CLI that uses the `@github/copilot-sdk` to autonomously execute specialized tasks on GitHub issues. Supports four specializations: `backend`, `frontend`, `security`, `testing`.
2. **Persona Pack** (`personas/full-lifecycle-personas.json`) — 40 prompt personas across 8 SDLC lifecycle phases (portfolio orchestration, vision/strategy, architecture/engineering, quality/delivery, operations, marketing/growth, governance).
3. **KEDA External Scaler** (`keda/github-issue-external-scaler/`) — Go service that converts GitHub issue webhooks into KEDA scale signals, driving Kubernetes-based worker pod autoscaling.

## Build and run

### Issue worker (Node.js)

```bash
npm install
node src/issue-worker.js --repo owner/repo --issue 123 --specialization backend --model gpt-5.3-codex
```

There are no test, lint, or build scripts defined.

### KEDA scaler (Go 1.22)

```bash
cd keda/github-issue-external-scaler
go build -o scaler .
```

Docker build (multi-stage, distroless):

```bash
docker build -t github-issue-external-scaler keda/github-issue-external-scaler/
```

## Architecture

### Issue worker flow

`parseArgs()` → `fetchIssueContext()` → `buildPrompt()` → `loadCopilotSdk()` → `runWorker()`

- CLI args are parsed with a custom map-based parser (no external arg library).
- Issue context is fetched via the `gh` CLI (`gh api`), pulling the issue plus last 8 comments.
- `buildPrompt()` constructs a role-specific system prompt using a specialization profile (objective + focus areas).
- `loadCopilotSdk()` dynamically imports `@github/copilot-sdk` and adapts to multiple API shapes.
- `runWorker()` invokes `client.run()` with sandbox mode `workspace-only`.

### KEDA scaler architecture

Dual-server Go binary:

- **HTTP server** (`:8080`): Receives GitHub `issues` webhooks, validates HMAC-SHA256 signatures, increments/decrements an `atomic.Int64` pending counter on `opened`/`reopened`/`closed` events.
- **gRPC server** (`:9090`): Implements the KEDA ExternalScaler interface (`IsActive`, `StreamIsActive`, `GetMetricSpec`, `GetMetrics`), streaming active status every 2 seconds.

State is in-memory only (no persistence). The `GITHUB_FILTER_REPO` env var optionally restricts to a single repository.

### Kubernetes deployment

Manifests in `keda/github-issue-external-scaler/manifests/`:

- `external-scaler.yaml` — Deployment + Service in `keda` namespace
- `scaledobject.yaml` — KEDA ScaledObject targeting an `issue-worker` Deployment (min 0, max 10 replicas, 5s poll, 60s cooldown)

## Key conventions

- **ESM modules** throughout (`"type": "module"` in package.json, `import`/`export` syntax).
- **No external CLI framework** — the issue worker uses a hand-rolled argument parser mapping `--key value` pairs.
- **Persona schema** — each persona in `full-lifecycle-personas.json` has `id`, `phase`, `goal`, `prompt`, and `agent_instructions` (with `inputs`, `workflow_steps`, `outputs`, `guardrails`). Follow this schema when adding new personas.
- **Output contract** — persona outputs follow a shared template: summary, decisions, deliverables, risks, handoff.

## KEDA scaler environment variables

| Variable | Default | Purpose |
|---|---|---|
| `GRPC_ADDR` | `:9090` | gRPC listen address |
| `HTTP_ADDR` | `:8080` | Webhook listen address |
| `GITHUB_WEBHOOK_SECRET` | — | HMAC-SHA256 webhook validation |
| `GITHUB_FILTER_REPO` | — | Restrict to single repo (`owner/repo`) |
| `KEDA_METRIC_NAME` | `github_issue_events_pending` | Metric name exposed to KEDA |
| `KEDA_TARGET_VALUE` | `1` | Target metric value per replica |
