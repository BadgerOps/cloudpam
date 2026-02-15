# CloudPAM

**Cloud-native IP Address Management**

CloudPAM is a modern IPAM solution designed for hybrid and multi-cloud environments. It helps infrastructure teams plan, allocate, and track IP addresses across on-premises data centers and cloud providers.

## Key Features

- **Hierarchical Pool Management** - Organize IP addresses in a tree structure matching your network topology
- **Cloud Discovery** - Auto-import VPCs, subnets, and EIPs from AWS (single-account and Organizations mode)
- **Network Analysis** - Gap analysis, fragmentation scoring, and compliance checks
- **Recommendations** - Automated allocation and compliance recommendations with apply/dismiss workflow
- **Schema Wizard** - Design IP schemas with conflict detection before deploying
- **CIDR Search** - Unified search with containment queries across pools and accounts
- **Auth & RBAC** - Local user management with session cookies and API keys
- **Audit Logging** - Full activity tracking with filterable event log
- **Observability** - Structured logging (slog), Prometheus metrics, Sentry integration
- **Dark Mode** - Three-mode toggle (Light/Dark/System)

### Planned

- GCP and Azure cloud discovery
- AI-powered network planning with LLM integration
- Multi-tenancy with SSO/OIDC
- Distributed tracing (OpenTelemetry)

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
| **Backend** | Go 1.24 |
| **Database** | PostgreSQL 15+ (production) / SQLite (development) / In-memory (demo) |
| **Frontend** | React 18 + Vite + TypeScript + Tailwind CSS |
| **API** | OpenAPI 3.1 |
| **Auth** | Session cookies + API keys + RBAC |
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
| [Implementation Roadmap](docs/IMPLEMENTATION_ROADMAP.md) | 20-week phased development plan |
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

### Analysis & Recommendations
- `POST /api/v1/analysis` - Full network analysis report
- `POST /api/v1/analysis/gaps` - Gap analysis for a pool
- `POST /api/v1/analysis/fragmentation` - Fragmentation scoring
- `POST /api/v1/analysis/compliance` - Compliance checks
- `POST /api/v1/recommendations/generate` - Generate recommendations
- `GET /api/v1/recommendations` - List recommendations
- `POST /api/v1/recommendations/{id}/apply` - Apply a recommendation

### Auth & System
- `POST /api/v1/auth/login` - Session login
- `GET /api/v1/auth/me` - Current identity
- `GET /api/v1/auth/keys` - API key management
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
│   ├── http/               # HTTP server, routes, handlers, middleware
│   ├── storage/            # Store interface + implementations
│   │   ├── sqlite/         # SQLite implementation
│   │   └── postgres/       # PostgreSQL implementation
│   ├── discovery/          # Cloud resource discovery
│   │   └── aws/            # AWS collector (VPCs, subnets, EIPs, Organizations)
│   ├── planning/           # Analysis engine (gaps, fragmentation, compliance, recommendations)
│   ├── auth/               # Authentication, RBAC, sessions, API keys
│   ├── audit/              # Audit logging
│   ├── cidr/               # CIDR math utilities
│   ├── validation/         # Input validation
│   └── observability/      # Logging, metrics
├── ui/                     # React/Vite/TypeScript frontend
├── web/                    # Embedded frontend assets (go:embed)
├── migrations/             # SQL migrations (0001-0012)
├── deploy/                 # Deployment configurations
│   └── terraform/          # AWS Organizations discovery IAM
├── docs/                   # Project documentation + OpenAPI spec
├── .github/workflows/      # CI/CD (test, lint, release builds)
├── Justfile                # Task runner commands
└── CLAUDE.md               # AI assistant context
```

## Implementation Status

| Phase | Status | Description |
|-------|--------|-------------|
| Foundation (Sprints 1-4) | Complete | Auth, RBAC, audit, observability, rate limiting |
| Enhanced Models (Sprint 5) | Complete | Pool hierarchy, stats, utilization tracking |
| Code Quality (Sprint 6) | Complete | Handler split, sentinel errors, 80%+ coverage |
| API & Storage (Sprint 7) | Complete | OpenAPI spec, SQLite API key store |
| Schema Wizard (Sprint 8) | Complete | Schema planner with conflict detection |
| Frontend (Sprint 9) | Complete | Unified React/Vite/TypeScript SPA |
| Dark Mode (Sprint 10) | Complete | Three-mode toggle |
| Search (Sprint 11) | Complete | CIDR search with containment queries |
| User Management (Sprint 12) | Complete | Local users, dual auth |
| Cloud Discovery (Sprint 13) | Complete | AWS collector, sync service, approval workflow |
| Analysis Engine (Sprint 14) | Complete | Gap analysis, fragmentation, compliance |
| Recommendations (Sprint 15) | Complete | Allocation & compliance recs, scoring, apply/dismiss |
| AWS Organizations (Sprint 16b) | Complete | Org-mode agent, cross-account discovery, Terraform/CF modules |
| AI Planning | Planned | LLM integration, conversational planning |
| Enterprise | Planned | Multi-tenancy, SSO/OIDC |

See [Implementation Roadmap](docs/IMPLEMENTATION_ROADMAP.md) for the full development timeline.

## Development

### Prerequisites

- Go 1.24+
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
