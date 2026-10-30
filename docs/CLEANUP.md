# Documentation map

Which files under `docs/` are current, which are design, and which are historical. The implemented-vs-missing snapshot is [CURRENT_STATUS.md](CURRENT_STATUS.md).

## Current (operators and contributors)

| File | Use |
|------|-----|
| [CURRENT_STATUS.md](CURRENT_STATUS.md) | Implemented vs missing at 0.23.4 plus #315 (2026-10-29) |
| [CHANGELOG.md](CHANGELOG.md) | Released behaviour |
| [TERRAFORM_PROVIDER.md](TERRAFORM_PROVIDER.md) | Provider v1 (pools and accounts) |
| [SCREENSHOTS.md](SCREENSHOTS.md) | Regenerating `photos/` |
| [DISCOVERY.md](DISCOVERY.md) | AWS/GCP discovery operator guide (see CURRENT_STATUS for Azure/agent limits) |
| [AUTH_FLOWS.md](AUTH_FLOWS.md) | Local, OIDC, API key, RBAC (no MFA) |
| [SIEM_INTEGRATION.md](SIEM_INTEGRATION.md) | CEF syslog (shipped) plus remaining SIEM design |
| [DEPLOYMENT.md](DEPLOYMENT.md) | Production setup and Helm 0.2.0 upgrade guidance |

Runtime API contract: `/openapi` and `/openapi.yaml` on a running server. Do not maintain a snapshot `docs/openapi.yaml`.

## Design / target architecture (not a status report)

| File | Use |
|------|-----|
| [SMART_PLANNING.md](SMART_PLANNING.md) | Analysis and AI target architecture |
| [DATABASE_SCHEMA.md](DATABASE_SCHEMA.md) | Target schema; live SQL is `migrations/` and `migrations/postgres/` |
| [OBSERVABILITY.md](OBSERVABILITY.md) | slog/metrics/tracing/CEF mixed with Vector/Jaeger/HEC design |
| [IMPLEMENTATION_ROADMAP.md](IMPLEMENTATION_ROADMAP.md) | 24-week plan; week checkboxes are mixed history |
| [PROJECT_PLAN.md](PROJECT_PLAN.md) | Original plan plus a current-state summary |
| [DISCOVERY_AGENT_PLAN.md](DISCOVERY_AGENT_PLAN.md) | Agent separation plan (the agent shipped; GCP agent in the diagram did not) |
| [UI_ENHANCEMENT.md](UI_ENHANCEMENT.md) | Early UI research |
| [API_EXAMPLES.md](API_EXAMPLES.md) | Design-era curls with the current auth header. Most paths are not served, so use `/openapi` for the real API |
| [design/](design/), [plans/](plans/), [superpowers/plans/](superpowers/plans/) | Dated design notes |

## Historical

| File | Use |
|------|-----|
| [REVIEW.md](REVIEW.md) | October 2025 review; P0/P1 listed there are resolved |
| [openapi-smart-planning.yaml](openapi-smart-planning.yaml), [openapi-observability.yaml](openapi-observability.yaml) | Design OpenAPI; not the runtime spec |
| [SPRINT5_PLAN.md](SPRINT5_PLAN.md) | Sprint 5 plan |
| [issues/cloud-run-deployment.md](issues/cloud-run-deployment.md) | Unbuilt Cloud Run proposal |
| `HANDOFF.md` (repo root) | Sprint 14 snapshot; bootstrap remaining-work is obsolete |
