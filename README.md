# CloudPAM

**Cloud-native IP Address Management**

CloudPAM is a modern IPAM solution designed for hybrid and multi-cloud environments. It helps infrastructure teams plan, allocate, and track IP addresses across on-premises data centers and cloud providers.

## Key Features

- **Hierarchical Pool Management** - Organize IP addresses in a tree structure matching your network topology
- **Cloud Discovery** - Auto-import AWS VPCs/subnets/EIPs, AWS Organizations accounts, and GCP networks/subnetworks/external IPs
- **Drift Detection** - Compare discovered cloud resources against managed pools and resolve or ignore drift items
- **Network Analysis** - Gap analysis, fragmentation scoring, and compliance checks
- **Recommendations** - Automated allocation and compliance recommendations with apply/dismiss workflow
- **Schema Wizard** - Design IP schemas with conflict detection before deploying
- **AI Planning** - Optional OpenAI-compatible conversational planner with SSE streaming and plan apply
- **CIDR Search** - Unified search with containment queries across pools and accounts
- **Auth & RBAC** - Local users, session cookies, API keys, and optional OIDC/SSO
- **Release Notes & Updates** - Embedded changelog plus release-check and host-managed upgrade endpoints
- **Audit Logging** - Full activity tracking with filterable event log
- **Observability** - Structured logging (slog), Prometheus metrics, Sentry integration
- **Dark Mode** - Three-mode toggle (Light/Dark/System)

### Planned

- Azure cloud discovery
- Active multi-tenant organization isolation and org management
- Distributed tracing (OpenTelemetry)
- External log destinations and SIEM forwarding

## Quick Start

```bash
# Clone the repository
git clone https://github.com/BadgerOps/cloudpam.git
cd cloudpam

# Run with in-memory store (no dependencies needed)
just dev

# Or run directly
go run ./cmd/cloudpam

# Access the UI
open http://localhost:8080
```

For SQLite persistence:

```bash
just sqlite-run
```

See the [Deployment Guide](docs/DEPLOYMENT.md) for production setup.

## Technology Stack

| Component | Technology |
|-----------|------------|
| **Backend** | Go 1.25 |
| **Database** | PostgreSQL 15+ (production) / SQLite (development) / In-memory (demo) |
| **Frontend** | React 18 + Vite + TypeScript + Tailwind CSS |
| **API** | OpenAPI 3.1 |
| **Auth** | Local sessions + API keys + OIDC + RBAC |
| **Logging** | slog (Go std lib) |
| **Metrics** | Prometheus |
| **Error Tracking** | Sentry (backend + frontend) |

## Documentation

### Getting Started
| Document | Description |
|----------|-------------|
| [Deployment Guide](docs/DEPLOYMENT.md) | Production deployment options |
| [API Examples](docs/API_EXAMPLES.md) | Common API usage patterns |
| [Cloud Discovery](docs/DISCOVERY.md) | AWS discovery setup and API reference |

### Architecture & Design
| Document | Description |
|----------|-------------|
| [API Specification](docs/openapi.yaml) | OpenAPI 3.1 spec |
| [Database Schema](docs/DATABASE_SCHEMA.md) | PostgreSQL/SQLite schema design |
| [Authentication Flows](docs/AUTH_FLOWS.md) | Session, API key, and RBAC flows |
| [Smart Planning Architecture](docs/SMART_PLANNING.md) | Analysis engine and AI planning design |
| [Observability Architecture](docs/OBSERVABILITY.md) | Logging, metrics, tracing, audit |
| [Implementation Roadmap](docs/IMPLEMENTATION_ROADMAP.md) | Historical phased roadmap with a current status refresh |
| [Code Review](docs/REVIEW.md) | Code review with prioritized issues |
| [Discovery Agent Plan](docs/DISCOVERY_AGENT_PLAN.md) | Standalone discovery agent architecture |

## API Overview

CloudPAM provides a REST API served at `/api/v1/`. The OpenAPI spec is available at `/openapi.yaml` when running.

### Core Resources
- `GET/POST /api/v1/pools` - Pool management (CRUD, hierarchy, stats)
- `GET/POST /api/v1/accounts` - Cloud account management
- `GET /api/v1/blocks` - List assigned blocks with filters
- `GET /api/v1/search` - Unified search with CIDR containment queries

### Cloud Discovery
- `GET /api/v1/discovery/resources` - List discovered cloud resources
- `POST /api/v1/discovery/sync` - Trigger cloud sync
- `POST /api/v1/discovery/ingest/org` - Bulk AWS Organizations ingest
- `POST /api/v1/drift/detect` - Run drift detection against discovered resources
- `GET /api/v1/drift` - List drift items and summary data

### Analysis & Recommendations
- `POST /api/v1/analysis` - Full network analysis report
- `POST /api/v1/analysis/gaps` - Gap analysis for a pool
- `POST /api/v1/analysis/fragmentation` - Fragmentation scoring
- `POST /api/v1/analysis/compliance` - Compliance checks
- `POST /api/v1/recommendations/generate` - Generate recommendations
- `GET /api/v1/recommendations` - List recommendations
- `POST /api/v1/recommendations/{id}/apply` - Apply a recommendation
- `POST /api/v1/ai/chat` - Stream an AI planning response
- `GET/POST /api/v1/ai/sessions` - Manage AI planning sessions

### Auth & System
- `POST /api/v1/auth/login` - Session login
- `GET /api/v1/auth/me` - Current identity
- `GET /api/v1/auth/keys` - API key management
- `GET /api/v1/auth/oidc/providers` - List enabled OIDC providers
- `GET /api/v1/system/info` - Version, release, and upgrade metadata
- `GET /api/v1/updates` - Check for newer releases
- `GET /healthz` / `GET /readyz` - Health and readiness checks
- `GET /metrics` - Prometheus metrics

## Project Structure

```
cloudpam/
├── cmd/cloudpam/           # Main entrypoint and storage selection
│   ├── main.go             # Server startup, flags, graceful shutdown
│   ├── store_default.go    # In-memory store (default build)
│   ├── store_sqlite.go     # SQLite store (-tags sqlite)
│   └── store_postgres.go   # PostgreSQL store (-tags postgres)
├── internal/
│   ├── domain/             # Core types (Pool, Account, DiscoveredResource, etc.)
│   ├── api/                # HTTP server, routes, handlers, middleware
│   ├── storage/            # Store interface + implementations
│   │   ├── sqlite/         # SQLite implementation
│   │   └── postgres/       # PostgreSQL implementation
│   ├── discovery/          # Cloud resource discovery
│   │   ├── aws/            # AWS collector (VPCs, subnets, EIPs, Organizations)
│   │   └── gcp/            # GCP collector (networks, subnetworks, external IPs)
│   ├── planning/           # Analysis engine, recommendations, AI planning
│   ├── auth/               # Authentication, RBAC, sessions, API keys
│   ├── audit/              # Audit logging
│   ├── cidr/               # CIDR math utilities
│   ├── validation/         # Input validation
│   └── observability/      # Logging, metrics
├── ui/                     # React/Vite/TypeScript frontend
├── web/                    # Embedded frontend assets (go:embed)
├── migrations/             # SQLite + PostgreSQL schema migrations
├── deploy/                 # Deployment configurations
│   └── terraform/          # Discovery IAM and infrastructure helpers
├── docs/                   # Project documentation + OpenAPI spec
├── .github/workflows/      # CI/CD (test, lint, release builds)
├── Justfile                # Task runner commands
└── CLAUDE.md               # AI assistant context
```

## Implementation Status

| Area | Status | Notes |
|------|--------|-------|
| Core IPAM | Complete | Pools, accounts, blocks, import/export, search, validation, audit |
| Discovery: AWS | Complete | Single-account and AWS Organizations discovery, agent flow, IaC helpers |
| Discovery: GCP | Partial | Collector exists for networks, subnetworks, and external IPs; AWS workflow/docs are more mature |
| Drift Detection | Complete | Unmanaged resource, CIDR mismatch, and orphaned discovered-pool detection with resolve/ignore workflow |
| Smart Planning | Complete | Analysis, recommendations, and schema planner are implemented |
| AI Planning | Complete, optional | OpenAI-compatible backend, SSE chat, stored sessions, plan extraction, and apply-plan flow |
| Auth & SSO | Complete | Local auth, sessions, API keys, OIDC provider management, JIT provisioning, local-auth toggle |
| Operations | Partial | Metrics, Sentry, release notes, and host-managed upgrades are implemented; tracing and log destinations are not |
| Multi-tenancy | Planned | PostgreSQL schema has default-org scaffolding, but the app still runs as single-tenant |

See [Implementation Roadmap](docs/IMPLEMENTATION_ROADMAP.md) for the full development timeline.

## Development

### Prerequisites

- Go 1.25+
- Node.js 18+ (for frontend development)
- [Just](https://github.com/casey/just) command runner

### Common Commands

```bash
just dev              # Run server on :8080 (in-memory store)
just build            # Build binary
just sqlite-build     # Build with SQLite support
just test             # Run all tests
just test-race        # Run tests with race detector
just lint             # Run golangci-lint
just fmt              # Format code
just cover            # Generate coverage report
```

### Frontend Development

```bash
cd ui && npm install        # Install dependencies
cd ui && npm run dev        # Vite dev server (proxied to :8080)
cd ui && npm run build      # Production build -> web/dist/
cd ui && npx vitest run     # Run tests
```

## Contributing

We welcome contributions! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/BadgerOps/cloudpam/issues)
- **Discussions**: [GitHub Discussions](https://github.com/BadgerOps/cloudpam/discussions)
