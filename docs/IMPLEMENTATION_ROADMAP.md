# CloudPAM Implementation Roadmap

## Executive Summary

CloudPAM is an intelligent IP Address Management (IPAM) platform designed to manage, analyze, and optimize network infrastructure across cloud providers and on-premises environments. The platform combines intelligent discovery, gap analysis, and AI-powered planning to provide organizations with comprehensive IP address lifecycle management.

**Project Vision**: Enable organizations to optimize their IP address allocation strategies through intelligent analysis, automated discovery, and AI-assisted planning.

**Key Goals**:
1. Centralize IP address management across multi-cloud and hybrid environments
2. Provide intelligent analysis and recommendations for address space optimization
3. Enable natural language planning through AI integration
4. Support enterprise-scale deployments with multi-tenancy and SSO
5. Maintain complete audit trails for compliance and governance

**Timeline**: 20 weeks (5 phases)
**Target Audience**: Enterprise network teams, cloud architects, infrastructure engineers

---

## Phase 1: Foundation (Weeks 1-4)

**Objective**: Build core API infrastructure, authentication, database layer, and basic pool management operations.

### Week 1-2: Project Setup & Core Infrastructure

**Activities**:
- Initialize Go project structure with module dependencies
- Set up development environment (Docker Compose for PostgreSQL/SQLite)
- Implement CI/CD pipeline (GitHub Actions)
- Create HTTP server framework with chi router
- Set up logging, metrics, and error handling middleware
- Implement database abstraction layer supporting both PostgreSQL and SQLite

**Deliverables**:
- [x] Git repository with proper structure
- [x] Docker Compose development environment
- [x] HTTP server running on port 8080
- [x] Database connection pool with support for both PostgreSQL and SQLite
- [x] Observability framework (see [OBSERVABILITY.md](OBSERVABILITY.md)):
  - [x] Structured logging with slog (JSON output)
  - [x] OpenTelemetry metrics (Prometheus export)
  - [ ] Distributed tracing (Jaeger) — not yet implemented
  - [x] Health check endpoints (`/healthz`, `/readyz`)
- [ ] Local observability stack (see [docker-compose.observability.yml](deploy/docker-compose.observability.yml)) — config exists but not integrated

**Success Criteria**:
- Server starts cleanly and responds to `/health` endpoint
- Database migrations run successfully on both database types
- All errors properly logged and tracked
- Code builds and tests run in CI/CD

### Week 2-3: Database Layer & Migrations

**Activities**:
- Implement database migration system (versioned SQL files)
- Create all core tables: organizations, users, roles, permissions, pools
- Implement PostgreSQL and SQLite migrations
- Create indexes and constraints for optimal query performance
- Implement audit event logging with triggers
- Build database abstraction layer

**Deliverables**:
- [x] Complete schema migrations for PostgreSQL
- [x] Complete schema migrations for SQLite
- [x] Migration runner in Go
- [x] Database initialization scripts
- [x] Performance indexes on all key queries

**Success Criteria**:
- Migrations run without errors on both database types
- All indexes created correctly
- Audit triggers fire on data mutations
- Schema passes validation with database tools

### Week 3-4: Authentication & Authorization

**Activities**:
- Implement OAuth2/OIDC provider abstraction
- Build session management with encryption
- Implement API token generation and validation
- Create RBAC middleware and permission checking
- Build user provisioning system
- Implement audit logging for auth events

**Deliverables**:
- [ ] OIDC provider configuration interface — not yet implemented (local auth only)
- [x] Session store with encryption
- [x] API token generation and storage (Argon2id hashing)
- [x] Auth middleware stack
- [x] Permission validation middleware
- [x] User provisioning endpoints
- [x] Auth audit logging

**Success Criteria**:
- Successfully authenticate via OIDC (test with Keycloak locally)
- API keys generate and validate correctly
- Permission checks block unauthorized requests
- All auth events logged to audit table
- Session tokens refresh automatically

### Week 4: Basic Pool CRUD Operations

**Activities**:
- Implement pool entity and repository layer
- Create pool CRUD endpoints (POST, GET, PATCH, DELETE)
- Implement hierarchical pool operations (create sub-pools)
- Build pool querying with filters and pagination
- Implement materialized path for efficient hierarchy queries
- Add soft delete support

**Deliverables**:
- [x] Pool repository with full CRUD operations
- [x] REST endpoints: `/api/v1/pools/*`
- [x] Hierarchical pool creation and navigation
- [x] Pool search and filtering
- [x] Request/response validation
- [x] Comprehensive error handling

**Success Criteria**:
- Create root pool with CIDR
- Create child pools within parent
- Query pools with pagination
- Update pool properties
- Soft delete and recovery
- All operations audit logged

**Phase 1 Success Metrics**:
- Core infrastructure passes all tests
- Authentication tested with real OIDC provider
- 100+ API requests/second sustained on basic operations
- Database handles 10,000 pools without performance degradation

---

## Phase 2: Cloud Integration (Weeks 5-8)

**Objective**: Integrate with AWS/GCP/Azure, implement discovery sync engine, and enable drift detection.

### Week 5: Cloud Account Management

**Activities**:
- Design cloud provider abstraction layer
- Implement AWS IAM role assumption flow
- Implement GCP service account authentication
- Implement Azure managed identity flow
- Build account registration endpoints
- Create account credential encryption system
- Implement health checks and connectivity testing

**Deliverables**:
- [x] Cloud account entity and repository
- [ ] OAuth2 flows for each cloud provider — not yet implemented
- [ ] Credential encryption/decryption system — not yet implemented
- [x] Account management endpoints: `/api/v1/accounts/*`
- [x] Health check endpoints for connected accounts
- [x] Account listing with status

**Success Criteria**:
- Register AWS account with IAM role
- Register GCP account with service account
- Register Azure account with managed identity
- Health checks return accurate status
- Credentials stored encrypted

### Week 6: Discovery Collector Framework

**Activities**:
- Design collector abstraction and collector SDK
- Implement AWS EC2/VPC discovery collector
- Implement GCP Compute discovery collector
- Implement Azure networking discovery collector
- Build discovery data models and storage
- Create collector registration and heartbeat system
- Implement concurrent discovery with rate limiting

**Deliverables**:
- [x] Collector interface and SDK
- [x] AWS collector (VPCs, subnets, EIPs) + AWS Organizations cross-account discovery
- [ ] GCP collector (networks, subnetworks, IPs) — not yet implemented
- [ ] Azure collector (VNets, subnets, NICs) — not yet implemented
- [x] Discovery resource storage
- [x] Collector registry and heartbeat
- [x] Rate limiting per collector

**Success Criteria**:
- AWS collector discovers resources in test accounts
- GCP collector discovers resources in test projects
- Azure collector discovers resources in test subscriptions
- Discovered resources stored with metadata
- Collectors send heartbeats every 5 minutes
- Handles 1000+ resources per discovery cycle

### Week 7: Discovery Sync Engine

**Activities**:
- Implement sync job scheduling and execution
- Build incremental sync with change detection
- Implement resource reconciliation logic
- Create resource linking to pools
- Build conflict detection system
- Implement orphan detection
- Create sync job monitoring and retry logic

**Deliverables**:
- [x] Sync job scheduler (on-demand via API)
- [ ] Incremental sync engine — not yet implemented
- [x] Resource reconciliation against existing pools
- [x] Conflict detection and reporting
- [ ] Orphan resource detection — not yet implemented
- [x] Sync job history and logging
- [ ] Retry mechanism for failed syncs — not yet implemented

**Success Criteria**:
- Automatic sync runs every 60 minutes (configurable)
- Detects new, updated, and deleted resources
- Matches discovered resources to pools
- Identifies conflicts and orphans
- Maintains sync history for auditing
- Sync completes in < 5 minutes for 10k resources

### Week 8: Drift Detection & Cloud Account UI

**Activities**:
- Implement drift detection logic (discovered vs managed state)
- Build drift report generation
- Create cloud account UI scaffolding
- Implement account discovery status dashboard
- Build resource sync monitoring views
- Create reconciliation workflow UI

**Deliverables**:
- [ ] Drift detection engine — not yet implemented
- [ ] Drift reporting endpoints — not yet implemented
- [ ] Reconciliation suggestions — not yet implemented
- [x] Cloud account management UI
- [x] Sync status dashboard
- [x] Resource discovery browser

**Success Criteria**:
- Drift detection identifies all conflicts
- Reconciliation suggestions generated automatically
- UI shows sync status in real-time
- Historical sync data accessible and queryable

**Phase 2 Success Metrics**:
- Successfully integrate with all 3 major cloud providers
- Discover 10,000+ resources per account
- Sync completes within SLA (5 minutes for typical account)
- Drift detection accuracy > 99%
- Zero data loss during sync operations

---

## Phase 3: Smart Planning (Weeks 9-12)

**Objective**: Implement intelligent analysis engine, gap analysis, recommendations, and schema wizard.

### Week 9: Analysis Engine Core

**Activities**:
- Implement gap analysis algorithm
- Build fragmentation detection system
- Create utilization calculation engine
- Implement compliance rule engine
- Build analysis report generation
- Create caching for analysis results

**Deliverables**:
- [x] Gap analysis service
- [x] Fragmentation scoring algorithm
- [x] Compliance rule evaluation
- [x] Utilization metrics calculation
- [x] Analysis report endpoints: `/api/v1/analysis/*`
- [ ] Result caching with TTL — not yet implemented
- [x] Batch analysis for multiple pools

**Success Criteria**:
- Gap analysis identifies all unused address blocks
- Fragmentation scoring correlates with inefficiency
- Compliance checks validate against configurable rules
- Analysis runs in < 30 seconds for typical org
- Results cache for 1 hour

**Week 9 Details - Gap Analysis Algorithm**:
```
1. For each pool:
   a. Get all child allocations
   b. Identify unused ranges between allocations
   c. Score ranges by size and position
   d. Return available blocks sorted by suitability
2. Calculate utilization percentage
3. Project available runway (time until exhaustion)
```

### Week 10: Recommendation Generator

**Activities**:
- Build recommendation engine from analysis results
- Implement allocation suggestion algorithm
- Create consolidation recommendations
- Build compliance remediation suggestions
- Implement growth projection
- Create recommendation scoring and ranking

**Deliverables**:
- [x] Recommendation generation engine
- [x] Allocation suggestion algorithm
- [x] Consolidation detection
- [x] Compliance fix recommendations
- [ ] Growth projections (3-12 months) — not yet implemented
- [x] Recommendation ranking by impact
- [x] Auto-applicability detection (apply/dismiss workflow)

**Success Criteria**:
- Generates 5-20 recommendations per analysis
- Allocation suggestions ranked by quality
- Growth projections accurate within 80%
- Recommendations include rationale and impact
- Some recommendations auto-applicable

### Week 11: Schema Wizard & Templates

**Activities**:
- Design schema template system
- Build schema template library (5 common patterns)
- Implement schema generator from wizard inputs
- Create schema validation system
- Build interactive schema builder
- Implement schema visualization

**Deliverables**:
- [x] Schema template entities and storage
- [x] Built-in templates: Regional, Hierarchical, Flat, Cloud-Native, Hybrid
- [x] Schema wizard form endpoints
- [x] Schema generation from parameters
- [x] Schema validation with detailed errors (conflict detection)
- [ ] ASCII art visualization of generated schema — not implemented
- [ ] Schema export (JSON, YAML, Terraform) — not implemented

**Success Criteria**:
- 5+ built-in templates available
- Wizard generates valid schemas
- Validation catches overlaps and issues
- User can preview before applying
- Schema applies cleanly to create pool structure

**Week 12: Import/Export Functionality

**Activities**:
- Implement CSV import system
- Build Excel import support
- Create import validation and preview
- Implement JSON export
- Build YAML export for infrastructure-as-code
- Create Terraform provider export
- Implement bulk update from imports

**Deliverables**:
- [x] CSV import for accounts and pools
- [ ] Excel import — not implemented
- [x] Data validation and preview before commit
- [ ] Dry-run mode for all imports — not implemented
- [x] CSV/ZIP export endpoint
- [x] Error reporting on failed imports
- [ ] Import history and rollback — not implemented

**Success Criteria**:
- Import 1000+ pools from CSV in < 10 seconds
- Validate data integrity before committing
- Export generates valid infrastructure-as-code
- Dry-run mode catches all errors without changes

**Phase 3 Success Metrics**:
- Analysis engine completes in < 30 seconds
- Recommendations generated with > 85% actionability
- Schema wizard handles all 5 template types
- Import/export format compatibility with industry tools
- End-to-end planning workflow fully operational

---

## Phase 4: AI Planning (Weeks 13-16)

**Objective**: Implement LLM integration, conversational planning interface, and plan generation.

### Week 13: LLM Provider Abstraction

**Activities**:
- Design LLM provider interface
- Implement OpenAI provider
- Implement Anthropic provider
- Implement Azure OpenAI provider
- Implement Ollama (local) provider
- Create provider configuration system
- Build context injection system

**Deliverables**:
- [x] LLMProvider interface
- [x] OpenAI implementation (supports any OpenAI-compatible endpoint)
- [ ] Anthropic Claude implementation — not yet (uses OpenAI-compatible API)
- [x] Azure OpenAI implementation (via endpoint override)
- [x] Ollama implementation (via endpoint override)
- [x] Provider selection and fallback
- [x] Configuration validation

**Success Criteria**:
- Support all 5 LLM providers
- Seamless provider switching
- Proper error handling for provider failures
- Cost tracking for API-based providers
- Token usage monitoring

**Week 14: Conversational Planning Interface

**Activities**:
- Build conversation storage (PlanningConversation entity)
- Implement message handling and history
- Create system prompts for planning context
- Build context injection from pool data
- Implement message streaming responses
- Create conversation export functionality

**Deliverables**:
- [x] Conversation entity and repository
- [x] Message storage and retrieval
- [x] System prompt library (context-aware with pool/analysis data)
- [x] Context injection system
- [x] Streaming response handling (SSE)
- [x] Conversation endpoints: `/api/v1/ai/sessions/*`
- [ ] WebSocket support — used SSE streaming instead

**Success Criteria**:
- Maintain conversation history
- System prompts provide sufficient context
- LLM generates actionable responses
- Streaming responses feel responsive
- Conversations persist and retrievable

### Week 15: Plan Generation & Validation

**Activities**:
- Implement response parsing (extract PoolSpec from LLM output)
- Build plan generation from conversational context
- Create plan validation system
- Implement what-if analysis
- Build risk assessment for generated plans
- Create plan application workflow

**Deliverables**:
- [x] LLM response parser (plan extraction from markdown)
- [x] GeneratedPlan entity
- [x] Plan validation engine (via schema check)
- [ ] What-if simulation — not yet implemented
- [ ] Risk assessment algorithm — not yet implemented
- [x] Plan storage and retrieval (within conversations)
- [x] Plan application endpoints (`/api/v1/ai/sessions/{id}/apply-plan`)

**Success Criteria**:
- LLM generates valid pool specifications
- Validation catches overlaps and issues
- What-if analysis completes in < 5 seconds
- Risk assessment identifies conflicts
- Plans can be applied or discarded

### Week 16: Advanced Planning Features

**Activities**:
- Implement multi-turn planning with refinement
- Build plan comparison (original vs AI-generated)
- Create cost optimization analysis
- Implement growth factor consideration
- Build fallback recommendation system
- Create plan analytics and success tracking

**Deliverables**:
- [x] Multi-turn conversation refinement
- [ ] Plan comparison reports — not yet implemented
- [ ] Cost impact analysis — not yet implemented
- [ ] Growth headroom calculations — not yet implemented
- [x] Fallback to rule-based recommendations (recommendation engine)
- [ ] Plan success metrics tracking — not yet implemented
- [ ] Analytics endpoints — not yet implemented

**Success Criteria**:
- Users can refine plans through conversation
- Plan quality improves with iteration
- Cost analysis accurate and actionable
- Growth factors properly applied
- System gracefully falls back when LLM unavailable

**Phase 4 Success Metrics**:
- LLM integration stable and performant
- Conversational planning feel natural and intuitive
- Generated plans 90%+ actionable without modification
- Support 4+ concurrent planning conversations
- LLM cost < $0.10 per planning session average

---

## Phase 5: Enterprise Features (Weeks 17-20)

**Objective**: Enable multi-tenancy, enterprise authentication, audit compliance, and rate limiting.

### Week 17: Multi-Tenancy Implementation

**Activities**:
- Refactor data model for true multi-tenancy
- Implement organization isolation at database level
- Create organization management endpoints
- Build organization settings storage
- Implement organization-level quotas
- Create organization API key management

**Deliverables**:
- [x] Organization entity enhancements (schema exists, `defaultOrgID` hardcoded)
- [ ] Row-level security policies (PostgreSQL) — schema exists, not enforced
- [ ] Tenant isolation verification — not yet implemented
- [ ] Organization management UI/API — not yet implemented
- [ ] Settings storage per organization — not yet implemented
- [ ] Quota enforcement system — not yet implemented
- [ ] Organization billing hooks — not yet implemented

**Success Criteria**:
- Complete data isolation between organizations
- No cross-organization data leakage possible
- Organization admins can manage users and teams
- Quotas enforced (pools, syncs, API calls)
- Audit trail separated by organization

### Week 18: SSO/OIDC & Advanced Auth

**Activities**:
- Implement SSO configuration management
- Build OIDC provider discovery
- Create group mapping to roles
- Implement JIT (Just-In-Time) user provisioning
- Build session management UI
- Implement MFA support
- Create device management

**Deliverables**:
- [ ] SSO configuration endpoints — not yet implemented
- [ ] Provider discovery implementation — not yet implemented
- [ ] Group to role mapping UI — not yet implemented
- [ ] Auto-user provisioning system — not yet implemented
- [x] Session management endpoints (local sessions implemented)
- [ ] MFA enrollment and verification — not yet implemented
- [ ] Trusted device registration — not yet implemented

**Success Criteria**:
- SSO works with major IdPs (Okta, Azure AD, Google)
- Groups automatically map to roles
- New users auto-created on first login
- MFA support for high-security environments
- Sessions revocable per-device

### Week 19: Comprehensive Audit Logging & Log Shipping

**Activities**:
- Enhance audit event tracking for all operations
- Implement audit log search and filtering
- Build audit report generation
- Create compliance-ready exports
- Implement data retention policies
- Build audit log encryption for compliance
- Create audit analysis dashboard
- Deploy log shipping infrastructure (Vector sidecar)
- Configure SIEM integrations (Splunk, CloudWatch, Cloud Logging)

**Reference Documentation**:
- [OBSERVABILITY.md](OBSERVABILITY.md) - Full observability architecture
- [openapi-observability.yaml](openapi-observability.yaml) - Audit API endpoints
- [deploy/vector/vector.toml](deploy/vector/vector.toml) - Vector configuration
- [deploy/k8s/vector-daemonset.yaml](deploy/k8s/vector-daemonset.yaml) - K8s log collection
- [cloudpam-audit-log.html](cloudpam-audit-log.html) - UI mockup

**Deliverables**:
- [x] Comprehensive audit logging for all mutations
- [x] Audit search endpoints with filtering
- [ ] Audit report generation (JSON, CSV, PDF) — not yet implemented
- [ ] Data retention configuration — not yet implemented
- [ ] Audit trail encryption at rest — not yet implemented
- [ ] Compliance report templates — not yet implemented
- [x] Audit dashboard with analytics (frontend AuditPage)
- [ ] Vector sidecar deployment for log shipping — config exists in deploy/vector/
- [ ] SIEM integration (Splunk, CloudWatch, Cloud Logging) — not yet implemented

**Success Criteria**:
- Every mutation logged with before/after state
- Audit logs searchable and filterable
- Reports generated on demand
- Data retention policies enforced
- Logs encrypted with audit key
- Logs shipped to configured SIEM in real-time

### Week 20: Rate Limiting, Polish & Launch

**Activities**:
- Implement API rate limiting (per-user, per-org, per-key)
- Build rate limit quota management
- Create usage analytics dashboard
- Implement cost tracking and billing integration
- Polish UI/UX for general availability
- Comprehensive security audit
- Documentation and runbooks

**Deliverables**:
- [x] Rate limiting middleware (per-IP token bucket)
- [ ] Per-user rate limits — not yet implemented
- [ ] Per-API-key configurable limits — not yet implemented
- [ ] Usage dashboard — not yet implemented
- [ ] Cost tracking system — not yet implemented
- [ ] Security audit report — not yet implemented
- [x] Complete API documentation (OpenAPI 3.1 spec)
- [ ] Operations runbooks — not yet implemented

**Success Criteria**:
- Rate limits prevent abuse
- Fair quota system prevents resource starvation
- Usage tracking accurate
- System withstands 10k requests/second
- Zero security issues in audit

**Phase 5 Success Metrics**:
- Support 100+ organizations independently
- SSO deployment time < 30 minutes
- Audit logs queryable and compliant
- Rate limiting prevents abuse without impacting legitimate users
- System ready for production enterprise deployment

---

## Technical Dependencies

### Build-Before Dependencies

The following diagram shows which components must be completed before others:

```
Phase 1
├── Infrastructure (Weeks 1-2)
│   └── Required by: All phases
├── Database & Migrations (Weeks 2-3)
│   └── Required by: Auth, Pools
└── Auth (Weeks 3-4)
    └── Required by: All Phase 2+ features
    └── Basic Pools (Week 4)
        └── Required by: Phase 2, 3, 4

Phase 2
├── Cloud Account Management (Week 5)
│   └── Required by: Collectors, Sync
├── Collectors (Week 6)
│   └── Required by: Sync Engine
├── Sync Engine (Week 7)
│   └── Required by: Drift Detection
└── Drift Detection (Week 8)
    └── Required by: Analysis (Phase 3)

Phase 3
├── Analysis Engine (Week 9)
│   └── Required by: Recommendations, Reports
├── Recommendations (Week 10)
│   └── Required by: Planning UI
├── Schema Wizard (Week 11)
│   └── Parallel with Recommendations
└── Import/Export (Week 12)
    └── Required by: Phase 4 context injection

Phase 4
└── All depends on Phase 3 completion
    ├── LLM Abstraction (Week 13)
    │   └── Required by: Conversational Interface
    ├── Conversational Interface (Week 14)
    │   └── Required by: Plan Generation
    ├── Plan Generation (Week 15)
    └── Advanced Features (Week 16)

Phase 5
└── Can proceed in parallel with Phase 4
    ├── Multi-tenancy (Week 17)
    ├── SSO/OIDC (Week 18)
    ├── Audit Logging (Week 19)
    └── Rate Limiting (Week 20)
```

### Cross-Phase Dependencies

- **Phase 3 → Phase 4**: Analysis output provides context for AI planning
- **Phase 2 → Phase 3**: Discovered resources inform gap and fragmentation analysis
- **Phase 1 → All**: Authentication required by all features
- **Phase 3 → Phase 5**: Audit logging enhanced with enterprise features

---

## Risk Assessment

### Critical Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
| Cloud provider API changes | Medium | High | Maintain multiple SDK versions, abstract provider interfaces |
| Database migration failures in production | Low | Critical | Extensive testing, dry-run mode, automated rollback scripts |
| LLM provider unavailability | Medium | Medium | Multi-provider support, fallback to rule-based recommendations |
| Performance degradation at scale | Medium | High | Early load testing, caching strategy, database optimization |
| Security vulnerability in auth | Low | Critical | Regular security audits, penetration testing, bug bounty program |

### High Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
| Integration delays with cloud providers | Medium | Medium | Early prototyping with official SDKs, CloudFormation/Terraform |
| Data consistency issues across sync | Medium | High | Comprehensive reconciliation logic, audit trails, test harness |
| LLM output quality variance | Medium | Medium | Extensive prompt engineering, output validation, human review |
| API design changes | Low | Medium | Versioning strategy, deprecation notices, migration guides |

### Medium Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
| Third-party library vulnerabilities | Medium | Medium | Regular dependency updates, vulnerability scanning |
| Network latency affecting discovery | Medium | Low | Async operations, progress tracking, graceful degradation |
| User adoption of new features | Medium | Low | User education, documentation, gradual rollout |
| Team knowledge gaps on Go/PostgreSQL | Low | Medium | Training, documentation, code reviews, external expertise |

---

## Resource Requirements

### Development Team

**Team Composition** (Full-time equivalent):

| Role | Count | Weeks | Responsibility |
|------|-------|-------|-----------------|
| Senior Backend Engineer | 1 | 20 | Lead architecture, core APIs, database design |
| Backend Engineers | 2 | 20 | Feature implementation, cloud integration, testing |
| Frontend Engineer | 1 | 15 | UI implementation (Phases 2-5) |
| DevOps/Infrastructure | 1 | 20 | CI/CD, deployment, monitoring, database setup |
| QA Engineer | 1 | 15 | Test automation, integration testing (Phases 2-5) |
| Product Manager | 1 | 20 | Requirements, prioritization, stakeholder management |
| **Total** | **7** | **20 weeks** | |

**Optional Roles** (Part-time):
- Security Engineer (2 weeks): Phase 1, Phase 5 security audit
- Technical Writer (3 weeks): Documentation, API docs, runbooks
- Solutions Architect (2 weeks): Customer integration, design reviews

### Infrastructure Requirements

**Development Environment**:
- Cloud credits: ~$500/month (AWS/GCP/Azure for testing)
- Test databases: PostgreSQL and SQLite
- Local dev machine: 8GB RAM, 50GB disk minimum

**Staging Environment**:
- PostgreSQL instance (managed service)
- Cache layer (Redis) for session/analysis caching
- Load balancer for testing multi-instance
- Cloud accounts for each provider (test accounts)

**Production Deployment**:
- High-availability PostgreSQL (RDS, Cloud SQL, or managed)
- Kubernetes cluster (optional, for scaling)
- Cache layer (Redis) for performance
- API Gateway for rate limiting
- CDN for static assets

### Technology Stack

**Backend**:
- Language: Go 1.21+
- Web Framework: chi router
- Database: PostgreSQL (primary), SQLite (fallback)
- Migration Tool: golang-migrate
- Auth: go-oidc, jwt-go
- Cloud SDKs: AWS SDK v2, GCP client libraries, Azure SDK

**Frontend** (not detailed in roadmap, assumed separate):
- Framework: React or Vue
- HTTP Client: Axios
- State Management: Redux/Vuex
- UI Library: Material-UI or Tailwind CSS

**DevOps**:
- Containerization: Docker
- Orchestration: Docker Compose (dev), Kubernetes (production optional)
- CI/CD: GitHub Actions
- Monitoring: Prometheus + Grafana
- Logging: ELK Stack or cloud provider logs

---

## Success Metrics

### Phase 1: Foundation

| Metric | Target | Measurement |
|--------|--------|-------------|
| API Response Time | < 100ms p95 | APM monitoring |
| Test Coverage | > 80% | Coverage reports |
| Authentication Success Rate | > 99.9% | Auth logs |
| Database Query Performance | < 50ms avg | Query logs |
| Code Review Quality | > 90% approval rate | GitHub metrics |

### Phase 2: Cloud Integration

| Metric | Target | Measurement |
|--------|--------|-------------|
| Discovery Accuracy | > 99% of resources found | Manual verification |
| Sync Performance | < 5 min for 10k resources | Sync job logs |
| Cloud Provider Support | All 3 major clouds | Manual testing |
| Reconciliation Accuracy | > 98% | Audit comparison |
| Collector Uptime | > 99.5% | Heartbeat logs |

### Phase 3: Smart Planning

| Metric | Target | Measurement |
|--------|--------|-------------|
| Analysis Completion Time | < 30 seconds | Performance monitoring |
| Recommendation Quality | > 85% actionable | User feedback |
| Schema Generation | 100% valid schemas | Validation tests |
| Import Success Rate | > 99% | Import logs |
| Export Compatibility | Works with Terraform/YAML | Integration tests |

### Phase 4: AI Planning

| Metric | Target | Measurement |
|--------|--------|-------------|
| LLM Response Accuracy | > 90% valid plans | Output validation |
| Conversation Handling | 4+ concurrent conversations | Load testing |
| Plan Generation Time | < 30 seconds | Performance monitoring |
| LLM Provider Reliability | 99.5% availability | Provider monitoring |
| User Satisfaction | > 4/5 stars | User surveys |

### Phase 5: Enterprise Features

| Metric | Target | Measurement |
|--------|--------|-------------|
| Multi-tenant Isolation | 100% data separation | Security tests |
| SSO Deployment Time | < 30 minutes | Setup documentation |
| API Rate Limiting | Prevents abuse | Load tests |
| Audit Coverage | 100% of mutations | Audit logs |
| Compliance Readiness | Pass SOC 2 audit | External audit |

### Overall Product Metrics

| Metric | Phase | Target | Measurement |
|--------|-------|--------|-------------|
| Supported Pool Count | 3 | 10,000+ | Load testing |
| Concurrent Users | 5 | 1,000+ | Load testing |
| API Requests/Second | 2 | 1,000+ sustained | Load testing |
| System Uptime | 5 | 99.95% | Monitoring |
| Mean Time to Recovery | 5 | < 5 minutes | Incident response |
| Security Issues | All | 0 critical | Security audit |

---

## Implementation Timeline

### Gantt Chart Overview

```
Week   1  2  3  4  5  6  7  8  9 10 11 12 13 14 15 16 17 18 19 20
       |--|----|  Infrastructure & Auth
          |--|----|  Database & Pools
                     |--|----|  Cloud Integration (Accounts)
                        |--|----|  Collectors & Sync
                           |--|----|  Drift Detection & Analysis
                              |--|----|  Recommendations
                                 |--|----|  Schema Wizard
                                    |--|----|  AI Planning
                                       |--|----|  Advanced Features
          |--|--|--|--|--|--|--|--|--|--|--|--|--|--|  Multi-tenancy
                                       |--|--|--|  SSO
                                          |--|  Audit
                                             |--|  Rate Limit
```

### Key Milestones

- **End of Week 4**: Basic pool management operational, authentication working
- **End of Week 8**: Cloud discovery and sync fully functional
- **End of Week 12**: Complete analysis and planning capabilities
- **End of Week 16**: AI-powered planning conversation interface ready
- **End of Week 20**: Enterprise-ready product launch

---

## Development Standards & Best Practices

### Code Quality

- **Language**: Go 1.21+ with strict `go vet` and `golangci-lint`
- **Testing**: Unit tests (>80%), Integration tests, E2E tests
- **Documentation**: Godoc on all exported functions
- **Code Review**: All PRs require 2 approvals before merge
- **Commit Messages**: Conventional commits (feat:, fix:, docs:, etc.)

### Database Design

- **Transactions**: Critical operations wrapped in transactions with proper rollback
- **Migrations**: All schema changes via versioned migration files
- **Indexes**: Profile queries before going to production
- **Soft Deletes**: Use for audit trail preservation
- **Encryption**: Sensitive data (tokens, secrets) encrypted at rest

### API Design

- **Versioning**: API v1 with deprecation path for v2 features
- **RESTful**: Standard HTTP methods and status codes
- **Pagination**: Limit + offset for list endpoints
- **Validation**: Input validation at handler level
- **Error Responses**: Consistent error schema with error codes

### Security

- **HTTPS**: All production traffic encrypted
- **CORS**: Strict origin policy
- **CSRF**: Token validation on state-changing operations
- **Rate Limiting**: Prevent abuse
- **Logging**: No sensitive data in logs (tokens, passwords, credentials)
- **Dependencies**: Regular updates and vulnerability scanning

### Monitoring & Observability

- **Metrics**: Prometheus metrics for all critical paths
- **Logging**: Structured JSON logging with request tracing
- **Alerting**: Alerts for error rates, latency, availability
- **Tracing**: Distributed tracing for multi-component requests
- **Health Checks**: /health and /ready endpoints

---

## Post-Launch Roadmap (Months 5-12)

After initial launch, consider these enhancements:

### Month 5-6: Performance & Scale
- Database query optimization
- Caching strategy enhancements
- Horizontal scaling with load balancing
- CDN integration for static assets

### Month 7-8: Advanced Analytics
- Cost attribution per pool
- Historical trend analysis
- Capacity planning recommendations
- Chargeback reporting

### Month 9-10: Integration Ecosystem
- Terraform provider for CloudPAM
- Ansible inventory plugin
- CI/CD integrations (Jenkins, GitLab CI)
- Webhook support for events

### Month 11-12: Machine Learning
- Predictive growth models
- Anomaly detection in allocation patterns
- Cost optimization ML models
- Usage pattern analysis

---

## Appendix: Reference Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                         CloudPAM Architecture                         │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │
│  │   Frontend   │  │    Mobile    │  │     CLI      │               │
│  │   (React)    │  │   (Native)   │  │    (Go)      │               │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘               │
│         │                 │                  │                       │
│         └─────────────────┼──────────────────┘                       │
│                           │                                          │
│  ┌────────────────────────▼─────────────────────────────────────┐   │
│  │              API Gateway & Load Balancer                     │   │
│  │         (Rate Limiting, Auth, Routing)                       │   │
│  └────────────────────────┬─────────────────────────────────────┘   │
│                           │                                          │
│  ┌────────────────────────▼─────────────────────────────────────┐   │
│  │                    CloudPAM API (Go)                         │   │
│  ├──────────────────────────────────────────────────────────────┤   │
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐         │   │
│  │  │    Pool      │ │    Cloud     │ │   Analysis   │         │   │
│  │  │  Management  │ │ Integration  │ │   Engine     │         │   │
│  │  └──────┬───────┘ └──────┬───────┘ └──────┬───────┘         │   │
│  │         │                │                │                 │   │
│  │  ┌──────▼─────────────────▼──────────────▼──────┐           │   │
│  │  │        Smart Planning & AI Service          │           │   │
│  │  └──────────────────────────────────────────────┘           │   │
│  │                                                              │   │
│  └───────────────────────┬──────────────────────────────────────┘   │
│                          │                                           │
│  ┌───────────────────────▼────────────────────────────────────────┐ │
│  │              Data & Auth Layer                                │ │
│  ├──────────────────────────────────────────────────────────────┤ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │ │
│  │  │ PostgreSQL   │  │   Session    │  │   Audit     │      │ │
│  │  │   (Primary)  │  │    Store     │  │    Logs     │      │ │
│  │  └──────────────┘  └──────────────┘  └──────────────┘      │ │
│  │                                                              │ │
│  │  ┌──────────────┐  ┌──────────────┐                         │ │
│  │  │  Redis Cache │  │ OIDC Provider│                         │ │
│  │  └──────────────┘  └──────────────┘                         │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │              External Integrations                           │ │
│  ├──────────────────────────────────────────────────────────────┤ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │ │
│  │  │   AWS    │  │   GCP    │  │  Azure   │  │   LLMs   │   │ │
│  │  │          │  │          │  │          │  │(OpenAI, │   │ │
│  │  │VPC/EC2   │  │Compute/  │  │VNet/AKS  │  │ Claude) │   │ │
│  │  │          │  │Networks  │  │          │  │          │   │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

---

## Glossary

**CIDR**: Classless Inter-Domain Routing - notation for IP address ranges (e.g., 10.0.0.0/16)

**IPAM**: IP Address Management - system for managing and tracking IP addresses

**Drift Detection**: Identifying differences between desired state (managed pools) and actual state (cloud resources)

**Fragmentation**: Inefficient use of address space through scattered or oversized allocations

**Gap Analysis**: Identifying unused address blocks available for allocation

**OIDC**: OpenID Connect - authentication protocol

**RBAC**: Role-Based Access Control - authorization model based on user roles

**Soft Delete**: Marking records as deleted without removing from database (preserves audit trail)

**Materialized Path**: Database optimization technique for hierarchical data using path strings

**LLM**: Large Language Model (AI model like ChatGPT, Claude)

**Reconciliation**: Matching discovered resources with managed pools

**Schema**: Hierarchical structure of pools with naming and organization patterns

---

## Document Control

- **Version**: 1.1
- **Last Updated**: 2026-02-18
- **Owner**: CloudPAM Product Team
- **Status**: Phases 1-4 substantially complete; Phase 5 in progress

