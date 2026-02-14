# CloudPAM Development Handoff

**Last Updated**: 2026-02-14
**Branch**: `feature/discovery-agent-separation`
**Sprint**: Sprint 14 (Agent Auto-Registration) - IN PROGRESS

---

## Current Session Summary

This session implemented **Discovery Agent Separation** (Sprints 13-14), enabling remote discovery agents to run close to cloud resources and push data to the central server.

### ✅ Completed Work

#### **Phase 1-4: Discovery Agent Separation** (Sprints 13)
All 4 phases complete and committed:

1. **Server-Side APIs** (`2331e5f`)
   - Refactored `ProcessResources()` for shared sync logic
   - Added agent domain types, migration 0009
   - Extended DiscoveryStore with agent CRUD
   - HTTP handlers: ingest, heartbeat, list/get agents
   - RBAC: `discovery:create` for ingest/heartbeat, `discovery:read` for list/get

2. **Standalone Agent Binary** (`2f235a9`)
   - `cmd/cloudpam-agent/`: config, pusher, main entrypoint
   - Multi-region discovery (AWS collector)
   - Scheduler: periodic sync (15min) + heartbeat (1min)
   - Build commands: `just agent-build`, `just agent-run`

3. **Deployment & Packaging** (`b8b8b31`)
   - Docker: `deploy/docker/Dockerfile.agent` (multi-stage, non-root)
   - Helm chart: `deploy/helm/cloudpam-agent/` with IRSA/Workload Identity support
   - ConfigMap for agent.yaml, Secret for API key

4. **UI for Agent Health** (`7b565ca`)
   - Agents tab in Discovery page
   - Auto-refresh every 30s
   - Color-coded status badges (healthy/stale/offline)
   - Empty state with deployment instructions

#### **Sprint 14 Part 1: Bootstrap Token System** (`3d35932`)
Foundation for agent auto-registration:

- **Migration 0010**: Agent approval status, bootstrap_tokens table
- **Bootstrap tokens**: Generation, hashing (Argon2id), validation
- **Storage layer**: Bootstrap token CRUD (memory + SQLite)
- **Domain types**: `AgentApprovalStatus`, `BootstrapToken`, registration request/response types
- **Token features**: Expiration, use limits, revocation, account binding

### 🚧 Remaining Work (Sprint 14)

#### **High Priority - Backend**
- [ ] **Task #17**: Agent registration endpoint
  - `POST /api/v1/discovery/agents/register` (validates bootstrap token, creates agent, generates API key)
  - `POST /api/v1/discovery/bootstrap-tokens` (admin: create tokens)
  - `GET /api/v1/discovery/bootstrap-tokens` (admin: list tokens)
  - `DELETE /api/v1/discovery/bootstrap-tokens/{id}` (admin: revoke)

- [ ] **Task #18**: Approval workflow endpoints
  - `POST /api/v1/discovery/agents/{id}/approve`
  - `POST /api/v1/discovery/agents/{id}/reject`
  - Update agent status, set approved_by and approved_at

- [ ] **Task #19**: Config template endpoint
  - `GET /api/v1/discovery/agents/config-template?account_id=1`
  - Returns pre-filled YAML with server_url, account_id, placeholder for token

#### **High Priority - Agent**
- [ ] **Task #20**: Agent bootstrap registration flow
  - Check if API key exists; if not, use bootstrap token
  - Call `/api/v1/discovery/agents/register` with bootstrap token
  - Save returned API key to config file or environment
  - Fall back to existing flow if API key present

#### **Medium Priority - Frontend**
- [ ] **Task #21**: Deployment UI in Discovery page
  - Bootstrap token generator (admin only)
  - Copy-paste deployment commands (Docker, Kubernetes, Binary)
  - Pending agents section with approve/reject buttons
  - Config template download button
  - Visual workflow guide

#### **Low Priority - CI/CD**
- [ ] **Task #22**: GHCR publish workflow
  - `.github/workflows/publish-agent.yml`
  - Build and push on tags/releases
  - Multi-arch builds (amd64, arm64)

#### **Low Priority - Documentation**
- [ ] **Task #23**: `docs/AGENT_DEPLOYMENT.md`
  - Comprehensive deployment guide
  - Docker, Kubernetes, binary deployment
  - IAM/RBAC setup for AWS/GCP/Azure
  - Troubleshooting section

- [ ] **Task #24**: `docs/AGENT_REGISTRATION.md`
  - Registration workflow diagrams
  - Approval process
  - Bootstrap token best practices
  - Security considerations

---

## Repository State

### **Branch Structure**
```
master (main) ← feature/discovery-agent-separation (current)
```

**Commits on feature branch**:
```
3d35932 Bootstrap token system
7b565ca Phase 4: UI for agent health
b8b8b31 Phase 3: Deployment & packaging
2f235a9 Phase 2: Standalone agent binary
2331e5f Phase 1: Server-side APIs
```

### **Key Files Modified/Added**

#### Backend
- `migrations/0009_discovery_agents.sql` - Discovery agents table, sync job extensions
- `migrations/0010_agent_registration.sql` - Bootstrap tokens, agent approval status ⚠️ **NOT APPLIED YET**
- `internal/domain/discovery.go` - Agent and bootstrap token types
- `internal/discovery/bootstrap.go` - Token generation and validation
- `internal/discovery/collector.go` - Refactored ProcessResources()
- `internal/discovery/aws/collector.go` - Multi-region support
- `internal/storage/discovery.go` - Extended interface
- `internal/storage/discovery_memory.go` - Agent + token storage
- `internal/storage/sqlite/discovery.go` - Agent + token storage
- `internal/http/discovery_handlers.go` - Agent handlers

#### Agent Binary
- `cmd/cloudpam-agent/main.go` - Entrypoint, scheduler
- `cmd/cloudpam-agent/config.go` - YAML + env var config
- `cmd/cloudpam-agent/pusher.go` - HTTP client with retry

#### Deployment
- `deploy/docker/Dockerfile.agent` - Multi-stage build
- `deploy/helm/cloudpam-agent/` - Full Helm chart

#### Frontend
- `ui/src/api/types.ts` - Agent types
- `ui/src/hooks/useDiscovery.ts` - useDiscoveryAgents hook
- `ui/src/pages/DiscoveryPage.tsx` - Agents tab

#### Build
- `Justfile` - Added agent-build, agent-run commands

### **Database Migrations Status**
- ✅ 0001-0008: Applied (in production)
- ✅ 0009: Committed, ready to apply (`discovery_agents`, sync_jobs extensions)
- ⚠️ 0010: Committed, **NOT APPLIED YET** (bootstrap_tokens, agent status)

**To apply migrations**:
```bash
./cloudpam -migrate up
```

### **Environment Variables (New)**
```bash
# Server (optional)
CLOUDPAM_AUTO_APPROVE_AGENTS=true  # Skip approval step

# Agent (for bootstrap flow - not implemented yet)
CLOUDPAM_BOOTSTRAP_TOKEN=boot_xxxxx  # Instead of CLOUDPAM_API_KEY
```

---

## How to Continue Sprint 14

### **Option 1: Complete Registration Endpoint First (Recommended)**
Focus on getting the core backend working before UI/docs.

1. **Implement registration endpoint** (Task #17)
   - Create `internal/http/bootstrap_handlers.go`
   - Add handlers for token CRUD and agent registration
   - Wire up to routes with RBAC

2. **Implement approval endpoints** (Task #18)
   - Add approve/reject handlers
   - Update agent status in database

3. **Test with curl**:
   ```bash
   # Create bootstrap token
   curl -X POST localhost:8080/api/v1/discovery/bootstrap-tokens \
     -H "Authorization: Bearer $ADMIN_KEY" \
     -d '{"name":"test","account_id":1,"expires_in":"24h"}'

   # Agent registers
   curl -X POST localhost:8080/api/v1/discovery/agents/register \
     -d '{"name":"agent1","account_id":1,"bootstrap_token":"boot_xxx"}'

   # Approve agent
   curl -X POST localhost:8080/api/v1/discovery/agents/{id}/approve \
     -H "Authorization: Bearer $ADMIN_KEY"
   ```

4. **Update agent** (Task #20) to use bootstrap flow

5. **Build UI** (Task #21) for token management and approval

### **Option 2: Parallel Development**
If multiple developers:
- Dev A: Tasks #17-19 (backend endpoints)
- Dev B: Task #20 (agent bootstrap flow)
- Dev C: Task #21 (deployment UI)

### **Option 3: Minimal Viable Feature**
Ship without approval workflow for now:
1. Set `CLOUDPAM_AUTO_APPROVE_AGENTS=true` by default
2. Skip Task #18 (approval endpoints)
3. Skip pending agents UI in Task #21
4. Agents auto-register and immediately get API keys
5. Add approval workflow in Sprint 15

---

## Testing the Current Implementation

### **Test Agent Health Monitoring** (Phases 1-4)
```bash
# Terminal 1: Start server
just sqlite-run

# Terminal 2: Start UI
cd ui && npm run dev

# Browser: http://localhost:5173
# Navigate to Discovery > Agents tab
# Should see empty state with deployment instructions
```

### **Test Agent Deployment** (Phase 2-3)
```bash
# Create account with AWS credentials
# Set AWS_PROFILE or AWS_* env vars

# Create API key with discovery:create scope
curl -X POST localhost:8080/api/v1/auth/keys \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -d '{"name":"test-agent","scopes":["discovery:write"]}'

# Run agent
export CLOUDPAM_SERVER_URL=http://localhost:8080
export CLOUDPAM_API_KEY=cpam_xxx
export CLOUDPAM_ACCOUNT_ID=1
export CLOUDPAM_AGENT_NAME=test-agent
export CLOUDPAM_AWS_REGIONS=us-east-1,us-west-2

./cloudpam-agent

# Watch agent appear in UI with heartbeats
```

### **Test Bootstrap Tokens** (Sprint 14 Part 1)
Currently only storage layer - no HTTP endpoints yet.

---

## Technical Debt / Known Issues

1. **Migration 0010 not applied**: Need to run migrations before testing bootstrap flow
2. **Agent approval UI**: Needs pending agents section and approve/reject buttons
3. **Bootstrap token UI**: No admin interface for token management yet
4. **Agent bootstrap flow**: Agent still requires manual API key, doesn't auto-register
5. **GHCR publishing**: No CI/CD for agent container yet
6. **Documentation**: In-code only, need comprehensive docs

---

## Architecture Decisions

### **Why Bootstrap Tokens?**
- Avoids pre-creating API keys for every agent
- Enables approval workflow before granting full access
- Tokens can be time-limited and single-use
- Supports self-service agent deployment

### **Why Separate Agent Binary?**
- Cloud credentials stay with agents (security)
- Agents run in-region (performance, multi-region)
- Scales horizontally (multiple agents per account)
- Server doesn't need AWS/GCP/Azure credentials

### **Why Approval Workflow?**
- Prevents unauthorized agents from connecting
- Gives admins visibility into agent deployments
- Optional (can auto-approve with env var)
- Audit trail (approved_by, approved_at)

---

## Next Session Checklist

Before starting:
- [ ] Review this HANDOFF.md
- [ ] Check branch is up to date: `git pull origin feature/discovery-agent-separation`
- [ ] Apply migration: `./cloudpam -migrate up`
- [ ] Review task list: `just tasks` (if task tool available)
- [ ] Read `internal/discovery/bootstrap.go` for token logic

First tasks:
1. Create `internal/http/bootstrap_handlers.go`
2. Implement token CRUD handlers
3. Implement registration handler
4. Wire up routes in `server.go`
5. Test with curl

---

## Questions for Next Session

1. **Auto-approve by default?** Or require explicit approval?
2. **Token expiration defaults?** Suggest 24h, 7d, 30d?
3. **Max uses defaults?** Single-use tokens or unlimited?
4. **Account binding?** Require account_id on tokens or allow wildcard?
5. **UI location?** Separate "Agent Management" page or keep in Discovery tab?

---

## Useful Commands

```bash
# Build & Run
just sqlite-build
just agent-build
just dev-all  # Both backend + frontend

# Migrations
./cloudpam -migrate status
./cloudpam -migrate up

# Testing
go test ./...
cd ui && npm test

# Git
git log --oneline --graph feature/discovery-agent-separation ^master
git diff master...HEAD
```

---

## References

- Planning doc: `docs/DISCOVERY_AGENT_PLAN.md` (from previous session)
- Discovery docs: `docs/DISCOVERY.md`
- Roadmap: `IMPLEMENTATION_ROADMAP.md`
- Sprint tracking: Tasks #15-24 (use task tool if available)

---

**Ready to continue Sprint 14!** 🚀
