# CloudPAM

**Cloud-native IP Address Management**

CloudPAM is a modern IPAM solution designed for hybrid and multi-cloud environments. It helps infrastructure teams plan, allocate, and track IP addresses across on-premises data centers and cloud providers.

## Key Features

- **Hierarchical Pool Management** - Organize IP addresses in a tree structure matching your network topology
- **Smart Planning** - AI-assisted network planning with gap analysis, recommendations, and growth projections
- **Cloud Discovery** - Auto-import VPCs, subnets, and IPs from AWS, GCP, and Azure
- **Schema Wizard** - Design IP schemas with templates before deploying
- **Drift Detection** - Identify discrepancies between planned and actual allocations
- **Multi-tenant** - Role-based access control with team-scoped permissions

## Quick Start

```bash
# Clone the repository
git clone https://github.com/BadgerOps/cloudpam.git
cd cloudpam

# Start with Docker Compose (SQLite)
docker-compose up -d

# Access the UI
open http://localhost:8080
```

## Documentation

### Getting Started
| Document | Description |
|----------|-------------|
| [Quick Start Guide](internal/docs/content/getting-started/quick-start.md) | Get running in 5 minutes |
| [Deployment Guide](DEPLOYMENT.md) | Production deployment options |

### Architecture & Design
| Document | Description |
|----------|-------------|
| [API Specification](openapi.yaml) | OpenAPI 3.1 spec (85+ endpoints) |
| [Smart Planning API](openapi-smart-planning.yaml) | AI planning & analysis endpoints |
| [Database Schema](DATABASE_SCHEMA.md) | PostgreSQL/SQLite schema design |
| [Authentication Flows](AUTH_FLOWS.md) | JWT, API keys, SSO/OIDC |
| [Documentation Architecture](DOCUMENTATION_ARCHITECTURE.md) | Embedded docs system |

### Smart Planning
| Document | Description |
|----------|-------------|
| [Smart Planning Architecture](SMART_PLANNING.md) | Discovery, analysis, and AI planning |
| [Planning Interfaces](internal/planning/interfaces.go) | Go service interfaces |
| [Implementation Roadmap](IMPLEMENTATION_ROADMAP.md) | 20-week development plan |

### Observability
| Document | Description |
|----------|-------------|
| [Observability Architecture](OBSERVABILITY.md) | Logging, metrics, tracing, audit |
| [Observability Interfaces](internal/observability/interfaces.go) | Logger, Metrics, Tracer interfaces |
| [Observability API](openapi-observability.yaml) | Audit log and health endpoints |
| [Vector Configuration](deploy/vector/vector.toml) | Log shipping to Splunk, CloudWatch, etc. |
| [K8s Observability](deploy/k8s/observability-stack.yaml) | Prometheus, Grafana, Jaeger |
| [Docker Compose](deploy/docker-compose.observability.yml) | Local observability stack |

### User Guides
| Document | Description |
|----------|-------------|
| [IP Schema Planning](internal/docs/content/user-guide/ip-schema-planning.md) | Design effective IP schemas |
| [Smart Planning Guide](internal/docs/content/user-guide/smart-planning.md) | AI-assisted network planning |
| [API Examples](API_EXAMPLES.md) | Common API usage patterns |

### UI Mockups
| Mockup | Description |
|--------|-------------|
| [Dashboard](cloudpam-dashboard.html) | Main dashboard with pool overview |
| [Cloud Accounts](cloudpam-accounts.html) | Cloud provider integration |
| [Discovery](cloudpam-discovery.html) | Cloud resource discovery |
| [Schema Planner](cloudpam-schema-planner.html) | Visual schema designer |
| [AI Planning Assistant](cloudpam-ai-planning.html) | Conversational planning interface |
| [Audit Log](cloudpam-audit-log.html) | Activity and change tracking |
| [Auth Settings](cloudpam-settings-auth.html) | Authentication configuration |

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CloudPAM                                        │
│                                                                              │
│  ┌────────────────┐  ┌────────────────┐  ┌────────────────────────────────┐ │
│  │    REST API    │  │    Web UI      │  │      AI Planning Assistant     │ │
│  │  (OpenAPI 3.1) │  │    (React)     │  │   (LLM-powered, pluggable)     │ │
│  └───────┬────────┘  └───────┬────────┘  └───────────────┬────────────────┘ │
│          │                   │                           │                   │
│  ┌───────┴───────────────────┴───────────────────────────┴────────────────┐ │
│  │                         Core Services                                   │ │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌───────────────────┐ │ │
│  │  │    Pools    │ │    Auth     │ │   Audit     │ │   Smart Planning  │ │ │
│  │  │  Management │ │   (RBAC)    │ │   Logging   │ │  (Analysis + AI)  │ │ │
│  │  └─────────────┘ └─────────────┘ └─────────────┘ └───────────────────┘ │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                    │                                         │
│  ┌─────────────────────────────────┴─────────────────────────────────────┐  │
│  │                   PostgreSQL / SQLite (dual-mode)                      │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
          │                    │                    │
          ▼                    ▼                    ▼
   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
   │     AWS     │      │     GCP     │      │    Azure    │
   │ (VPC/EC2)   │      │ (Compute)   │      │  (VNets)    │
   └─────────────┘      └─────────────┘      └─────────────┘
```

## Technology Stack

| Component | Technology |
|-----------|------------|
| **Backend** | Go 1.21+ |
| **Database** | PostgreSQL 15+ (production) / SQLite (development) |
| **Frontend** | React 18 + Tailwind CSS |
| **API** | OpenAPI 3.1 with Scalar documentation |
| **Auth** | JWT + OAuth/OIDC + API Keys |
| **AI** | Pluggable LLM (OpenAI, Anthropic, Azure, Ollama) |
| **Logging** | slog (Go std lib) + Vector for shipping |
| **Metrics** | OpenTelemetry + Prometheus |
| **Tracing** | OpenTelemetry + Jaeger |

## Project Structure

```
cloudpam/
├── cmd/
│   └── cloudpam/           # Main application entry point
├── internal/
│   ├── api/                # HTTP handlers and routing
│   ├── domain/             # Core domain models
│   │   └── models.go       # Pool, Address, User types
│   ├── storage/            # Database interfaces and implementations
│   │   ├── interfaces.go   # Repository interfaces
│   │   ├── postgres/       # PostgreSQL implementation
│   │   └── sqlite/         # SQLite implementation
│   ├── planning/           # Smart planning services
│   │   └── interfaces.go   # Discovery, Analysis, AI interfaces
│   ├── observability/      # Logging, metrics, tracing
│   │   └── interfaces.go   # Logger, Metrics, Tracer interfaces
│   ├── auth/               # Authentication and authorization
│   └── docs/               # Embedded documentation
├── deploy/                 # Deployment configurations
│   ├── vector/             # Vector log shipping config
│   ├── k8s/                # Kubernetes manifests
│   └── docker-compose.observability.yml
├── migrations/             # Database migrations
├── web/                    # React frontend
├── openapi.yaml            # Core API specification
├── openapi-smart-planning.yaml  # Planning API specification
└── docker-compose.yml      # Local development setup
```

## Implementation Status

| Phase | Status | Description |
|-------|--------|-------------|
| Foundation | 🟡 Design | Core API, database, auth |
| Cloud Integration | 🟡 Design | AWS/GCP/Azure discovery |
| Smart Planning | 🟡 Design | Analysis, recommendations |
| AI Planning | 🟡 Design | LLM integration, conversations |
| Observability | 🟡 Design | Logging, metrics, tracing, audit |
| Enterprise | ⚪ Planned | Multi-tenancy, SSO |

See [IMPLEMENTATION_ROADMAP.md](IMPLEMENTATION_ROADMAP.md) for the full development timeline.

## API Overview

CloudPAM provides a comprehensive REST API with 100+ endpoints:

### Core Resources
- `GET/POST /api/v1/pools` - Pool management
- `GET/POST /api/v1/cloud-accounts` - Cloud provider integration
- `GET /api/v1/discovery/resources` - Discovered cloud resources
- `GET /api/v1/audit/events` - Audit trail

### Smart Planning
- `POST /api/v1/planning/analyze` - Run network analysis
- `GET /api/v1/planning/recommendations` - Get recommendations
- `POST /api/v1/planning/ai/conversations` - AI planning sessions
- `POST /api/v1/planning/schema/generate` - Generate schemas

### Configuration
- `GET/PUT /api/v1/settings/llm` - LLM provider configuration
- `GET /api/v1/settings/llm/prompts` - Prompt templates

Full API documentation available at `/docs/api` when running.

## Contributing

We welcome contributions! Please see our contributing guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Support

- **Documentation**: Browse the `/docs` endpoint
- **Issues**: [GitHub Issues](https://github.com/BadgerOps/cloudpam/issues)
- **Discussions**: [GitHub Discussions](https://github.com/BadgerOps/cloudpam/discussions)
