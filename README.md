# SwarmyMcSwarmFace

Agentic SDLC toolkit with:

- Issue-specialized Copilot worker (`src/issue-worker.js`)
- Lifecycle persona prompt pack (`personas/full-lifecycle-personas.json`)
- KEDA external scaler for GitHub issue events (`keda/github-issue-external-scaler`)

## 1) Issue worker (Copilot SDK)

Run a specialized worker against a GitHub issue:

```bash
npm install
node src/issue-worker.js \
  --repo owner/repository \
  --issue 123 \
  --specialization backend \
  --model gpt-5.3-codex
```

Specializations: `backend`, `frontend`, `security`, `testing`.

## 2) Persona pack

`personas/full-lifecycle-personas.json` includes 20+ personas covering ideation, design, architecture, engineering, QA, release, operations, and marketing/growth.

## 3) KEDA scaler plugin

See:

- `keda/github-issue-external-scaler/README.md` (full deployment + validation runbook)
- `keda/github-issue-external-scaler/manifests/*.yaml` (deployment + ScaledObject)

This scaler listens to GitHub `issues` webhooks and triggers KEDA-driven scale-up for worker pods.
