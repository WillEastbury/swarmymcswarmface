# GitHub Issue External Scaler (KEDA)

Minimal external scaler that turns GitHub `issues` webhooks into KEDA scale signals.

## Architecture

1. GitHub sends `issues` events to `POST /webhook/github`.
2. Scaler updates an internal `pendingCount`:
   - `opened`, `reopened` => `+1`
   - `closed` => `-1` (floored at 0)
3. KEDA polls scaler gRPC (`:9090`) via `IsActive` + `GetMetrics`.
4. `ScaledObject` scales your target Deployment (for example `issue-worker`).

## Deploy (quick start)

```bash
# 1) Deploy scaler service
kubectl apply -f manifests/external-scaler.yaml

# 2) Deploy your scale target (example)
kubectl -n default create deploy issue-worker --image=busybox --dry-run=client -o yaml \
  | sed 's/replicas: 1/replicas: 0/' \
  | kubectl apply -f -
kubectl -n default patch deploy issue-worker --type='json' \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args","value":["sh","-c","sleep infinity"]}]'

# 3) Create ScaledObject
kubectl apply -f manifests/scaledobject.yaml
```

## Configuration

Environment variables used by scaler container:

- `HTTP_ADDR` (default `:8080`) webhook listener
- `GRPC_ADDR` (default `:9090`) KEDA external scaler endpoint
- `GITHUB_WEBHOOK_SECRET` (recommended) validates `X-Hub-Signature-256`
- `GITHUB_FILTER_REPO` (optional) accept only one repo, format `owner/repo`
- `KEDA_METRIC_NAME` (default `github_issue_events_pending`)
- `KEDA_TARGET_VALUE` (default `1`) target metric per replica

## GitHub Webhook setup

Repository Settings -> Webhooks:

- URL: `https://<public-host>/webhook/github`
- Content type: `application/json`
- Secret: same value as `GITHUB_WEBHOOK_SECRET`
- Events: **Issues**

## Validation

```bash
# scaler health
kubectl -n keda port-forward svc/github-issue-external-scaler 18080:8080
curl -s http://localhost:18080/healthz

# check scale activity
kubectl -n default get scaledobject github-issue-worker -w
kubectl -n default get deploy issue-worker -w
```

Send a signed `issues.opened` webhook payload; replicas should scale from `0` upward.

## Troubleshooting

- `no matches for kind ScaledObject`: install KEDA CRDs/operator first.
- `ErrImagePull`: fix scaler image reference or use in-cluster `go run` deployment.
- No scale reaction: verify `scalerAddress`, webhook signature, and `GITHUB_FILTER_REPO`.
- Metrics not changing: check scaler logs:
  `kubectl -n keda logs deploy/github-issue-external-scaler -f`

## Production hardening

- Replace in-memory counter with Redis/Postgres (survive restarts).
- Add idempotency by GitHub delivery ID to prevent duplicate increments.
- Run scaler behind authenticated ingress + TLS.
- Add structured logs + alerting for webhook failures.
