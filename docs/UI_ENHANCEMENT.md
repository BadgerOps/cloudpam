# CloudPAM UI Enhancement Requirements

## Document Status
**Stage:** Discovery (In Progress)
**Last Updated:** 2026-01-27

---

## User Research

### Target Users

| Role | Technical Level | Primary Tasks |
|------|-----------------|---------------|
| Network Engineers | Expert (CIDR-fluent) | Network planning, subnet allocation, troubleshooting |
| DevOps/Platform Engineers | Expert | Infrastructure provisioning, drift detection, capacity planning |

**Key insight:** Users are technical but contextual hints and descriptions still valuable for complex operations.

### Usage Patterns

| Scenario | Frequency | Mode |
|----------|-----------|------|
| With host tracking enabled | Daily | Quick lookups, utilization checks |
| Planning/implementing networks | Weekly/Monthly | Extended sessions, multiple tabs |
| Reviewing existing infrastructure | As needed | Audit, compliance, drift detection |
| Identifying manual/rogue resources | As needed | Investigation, cleanup |

**Access context:**
- Desktop only (no mobile/tablet requirements)
- No on-call/incident response scenarios

### Collaboration Model

- **Multi-user environment:** Yes
- **Authentication:** SSO integration required
- **Authorization model:** RBAC/ABAC
  - `admin` - Full access, user management, settings
  - `editor` - Create/modify pools, accounts, allocations
  - `viewer` - Read-only access to all data

---

### Current Pain Points

| Pain Point | Impact | UI Opportunity |
|------------|--------|----------------|
| Slow to find info | Wasted time, frustration | Fast global search, keyboard shortcuts |
| Subnet-only view | Can't troubleshoot IP issues | Host tracking, IP lookup |
| Stale data | Wrong decisions, conflicts | Real-time sync status, "last updated" indicators |
| No cloud visibility | Drift, manual resources unknown | Discovery status, "unmanaged" badges |
| Admin bottleneck | Slow allocation cycle | Self-service with guardrails |

### Current vs. Desired Workflow

**Current (Spreadsheet):**
```
User requests → Admin searches spreadsheet → Admin finds block →
Admin updates spreadsheet → Admin tells user → User provisions
```
*Problems: Slow, manual, single point of failure, no validation*

**Desired (CloudPAM):**
```
User searches available space → User selects block → System validates →
User allocates (or requests approval) → Provisioned automatically
```
*Benefits: Self-service, real-time, validated, audited*

---

## Requirements

### Functional Requirements
*(Continuing discovery)*

### Non-Functional Requirements

| Requirement | Priority | Notes |
|-------------|----------|-------|
| SSO Integration | P1 | OIDC/SAML support |
| Role-based access | P1 | Admin/Editor/Viewer minimum |
| Desktop-optimized | P1 | No mobile requirements |
| Contextual help | P2 | Hints, tooltips, descriptions for complex fields |

---

## Open Questions (Interview Tracking)

### Answered
- [x] Who are the users? → Network engineers, DevOps
- [x] Technical level? → Expert, but hints helpful
- [x] Usage frequency? → Daily with hosts, weekly/monthly for planning
- [x] Access context? → Desktop only
- [x] Collaboration? → Yes, needs SSO + RBAC

### Pending
- [ ] Key views/screens needed?
- [ ] Search specifics?
- [ ] Visualization preferences?
- [ ] Self-service vs admin workflow?
- [ ] Export/reporting needs?

---

## Interview Notes

### Session 1 - 2026-01-27

**Q: Who are the primary users?**
> Network engineers and DevOps - very technical, comfortable with CIDR, but hints and descriptions would help.

**Q: Usage patterns?**
> - With host lookup: daily use
> - Without: mainly planning and implementing networks
> - Also: review of existing implementations, identifying manually created resources that may cause issues

**Q: Access context?**
> Desktop only, no on-call/mobile needs

**Q: Collaboration needs?**
> Collaborative environment. Should consider SSO login and RBAC/ABAC for admin, editor, viewer roles.

**Q: Most common tasks (ranked)?**
> 1. Allocate new subnet to team/account
> 2. Review what's deployed vs. documented
> 3. Find available space in a supernet
> 4. Look up who owns an IP/subnet (which account, team)

**Q: Current pain points?**
> - Slow to find information
> - Everything is subnet-based (no IP-level visibility)
> - Data is stale/out of sync with reality
> - No visibility into actual cloud state

**Q: Info needed when planning allocation?**
> ALL of it:
> - Parent pool and utilization
> - Available block sizes
> - What's already allocated nearby
> - Which accounts/teams own adjacent space

**Q: Current allocation workflow?**
> 1. User requests a subnet
> 2. Network admin manually identifies available block
> 3. Admin annotates spreadsheet
> 4. Admin gives subnet to user
>
> **Problem:** Manual, slow, error-prone, admin bottleneck

---
