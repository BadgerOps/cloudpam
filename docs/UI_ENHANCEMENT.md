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
- [ ] Export/reporting needs?
- [ ] Keyboard shortcuts?
- [ ] Dark mode?

### Answered (Part 3)
- [x] Search behavior → All types: exact CIDR, partial, IP lookup, name, account, combinations. Multiple filters. Performance consideration needed.
- [x] Visualization → Combination: tree + table + visual blocks
- [x] Utilization display → Mock up options, then narrow down
- [x] Self-service model → Request/approve with RBAC guardrails (project, account, tier)

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

**Q: Search behavior - what should users be able to type?**
> ALL of these:
> - Exact CIDR: `10.1.2.0/24`
> - Partial/fuzzy: `10.1.` or `10.1.2`
> - IP address: `10.1.2.45` → find containing subnet
> - Name: `prod-vpc`, `team-payments`
> - Account: `aws:123456789012`
> - Combinations with multiple filters
>
> **Concern:** What's the performance impact?

**Q: Visualization preferences?**
> Combination approach: tree view + table + visual block diagram

**Q: Utilization display?**
> Try a few options and mock them up, then narrow down.

**Q: Self-service model?**
> Request/approve workflow with RBAC guardrails based on:
> - Project
> - Account
> - Tier (dev/staging/prod)

---

## UI Mockups

### Utilization Display Options

**Option A: Percentage Bar (Inline)**
```
┌─────────────────────────────────────────────────────────────────┐
│ Pool: prod-vpc-primary (10.0.0.0/16)                            │
│ Account: aws:123456789012 │ Region: us-east-1                   │
│                                                                 │
│ Utilization: ████████████████░░░░░░░░░░░░░░░░ 52% (134/256)    │
│              ▲ allocated                    ▲ available         │
└─────────────────────────────────────────────────────────────────┘
```

**Option B: Color-Coded Status Badge**
```
┌─────────────────────────────────────────────────────────────────┐
│ POOLS                                              [+ New Pool] │
├─────────────────────────────────────────────────────────────────┤
│ Name                    │ CIDR          │ Used  │ Status        │
│─────────────────────────│───────────────│───────│───────────────│
│ ▼ prod-vpc-primary      │ 10.0.0.0/16   │  52%  │ 🟢 Healthy    │
│   ├─ subnet-web         │ 10.0.1.0/24   │  78%  │ 🟡 Warning    │
│   ├─ subnet-api         │ 10.0.2.0/24   │  91%  │ 🔴 Critical   │
│   └─ subnet-db          │ 10.0.3.0/24   │  23%  │ 🟢 Healthy    │
│ ▶ dev-vpc               │ 10.1.0.0/16   │  12%  │ 🟢 Healthy    │
└─────────────────────────────────────────────────────────────────┘

Thresholds: 🟢 <70%  🟡 70-85%  🔴 >85%
```

**Option C: Visual Block Map (like disk partitions)**
```
┌─────────────────────────────────────────────────────────────────┐
│ prod-vpc-primary (10.0.0.0/16) - 256 /24 blocks                 │
├─────────────────────────────────────────────────────────────────┤
│ ┌────┬────┬────┬────┬────┬────┬────┬────┬────┬────┬────┬────┐   │
│ │████│████│████│░░░░│░░░░│░░░░│████│████│░░░░│░░░░│░░░░│░░░░│   │
│ │web │api │db  │    │    │    │logs│mon │    │    │    │    │   │
│ └────┴────┴────┴────┴────┴────┴────┴────┴────┴────┴────┴────┘   │
│  .1    .2   .3   .4   .5   .6   .7   .8   .9  .10  .11  .12     │
│                                                                 │
│ Legend: ████ Allocated  ░░░░ Available  ▓▓▓▓ Reserved           │
│                                                                 │
│ Click a block to allocate or view details                       │
└─────────────────────────────────────────────────────────────────┘
```

**Option D: Heatmap Dashboard (Overview)**
```
┌─────────────────────────────────────────────────────────────────┐
│ UTILIZATION HEATMAP                           [Last sync: 2m ago]│
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  aws:prod-123456    aws:dev-789012     gcp:project-alpha        │
│  ┌───┬───┬───┐      ┌───┬───┬───┐      ┌───┬───┬───┐            │
│  │🔴 │🟡 │🟢 │      │🟢 │🟢 │🟢 │      │🟡 │🟢 │🟢 │            │
│  │91%│72%│45%│      │23%│18%│31%│      │78%│42%│15%│            │
│  ├───┼───┼───┤      ├───┼───┼───┤      ├───┼───┼───┤            │
│  │🟢 │🟢 │░░ │      │🟢 │░░ │░░ │      │🟢 │🟢 │░░ │            │
│  │34%│28%│   │      │ 8%│   │   │      │55%│33%│   │            │
│  └───┴───┴───┘      └───┴───┴───┘      └───┴───┴───┘            │
│                                                                 │
│  3 pools need attention (>85% utilized)        [View Details →] │
└─────────────────────────────────────────────────────────────────┘
```

**Option E: Combined List + Sparkline**
```
┌─────────────────────────────────────────────────────────────────┐
│ Pool                  │ CIDR          │ Trend (30d)  │ Now      │
│───────────────────────│───────────────│──────────────│──────────│
│ prod-vpc/subnet-web   │ 10.0.1.0/24   │ ▁▂▃▄▅▆▇█    │ 78% 🟡   │
│ prod-vpc/subnet-api   │ 10.0.2.0/24   │ ▃▃▄▅▅▆▇█    │ 91% 🔴   │
│ prod-vpc/subnet-db    │ 10.0.3.0/24   │ ▂▂▂▃▃▃▃▃    │ 23% 🟢   │
│ dev-vpc/main          │ 10.1.0.0/24   │ ▁▁▁▂▂▂▂▁    │ 12% 🟢   │
└─────────────────────────────────────────────────────────────────┘
```

### Search Interface Mockup

```
┌─────────────────────────────────────────────────────────────────┐
│ 🔍 Search: [10.1.2                                    ] [⌘K]    │
├─────────────────────────────────────────────────────────────────┤
│ Filters: [Account ▼] [Region ▼] [Tier ▼] [Status ▼] [+ Filter] │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 📁 POOLS matching "10.1.2"                                      │
│    ├─ 10.1.2.0/24   dev-api-subnet    aws:dev-789012   us-west-2│
│    └─ 10.1.20.0/22  staging-vpc       gcp:staging      us-cent1 │
│                                                                 │
│ 🖥️ HOSTS matching "10.1.2" (requires host tracking)            │
│    ├─ 10.1.2.45     i-0abc123  prod-api-server-3    Running     │
│    ├─ 10.1.2.67     i-0def456  prod-api-server-7    Running     │
│    └─ 10.1.2.89     eni-789    (unattached)         Available   │
│                                                                 │
│ Press Enter to search, Tab to cycle results, Esc to close       │
└─────────────────────────────────────────────────────────────────┘
```

### Allocation Request Flow Mockup

```
Step 1: Find Space
┌─────────────────────────────────────────────────────────────────┐
│ REQUEST NEW SUBNET                                    [?] Help  │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Parent Pool:    [prod-vpc-primary (10.0.0.0/16)          ▼]    │
│ Size needed:    [/24 - 254 usable hosts                  ▼]    │
│ Account:        [aws:123456789012 (prod)                 ▼]    │
│                                                                 │
│ ─────────────────────────────────────────────────────────────── │
│ AVAILABLE BLOCKS (12 of 256 /24s free)                          │
│                                                                 │
│ ┌────────────────────────────────────────────────────────────┐  │
│ │ ○ 10.0.4.0/24   │ ○ 10.0.5.0/24   │ ○ 10.0.6.0/24         │  │
│ │ ● 10.0.9.0/24   │ ○ 10.0.10.0/24  │ ○ 10.0.11.0/24  ←sel  │  │
│ │ ○ 10.0.12.0/24  │ ○ 10.0.13.0/24  │ ○ 10.0.14.0/24        │  │
│ └────────────────────────────────────────────────────────────┘  │
│                                                                 │
│ Selected: 10.0.9.0/24                                           │
│                                                    [Continue →] │
└─────────────────────────────────────────────────────────────────┘

Step 2: Request Details
┌─────────────────────────────────────────────────────────────────┐
│ REQUEST NEW SUBNET                              Step 2 of 3     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Selected Block: 10.0.9.0/24 (254 usable hosts)                  │
│                                                                 │
│ Name:           [payment-service-subnet                    ]    │
│ Purpose:        [Production payment processing API         ]    │
│ Tier:           [Production                              ▼]    │
│ Team:           [Payments (payments@company.com)         ▼]    │
│                                                                 │
│ ─────────────────────────────────────────────────────────────── │
│ APPROVAL REQUIRED                                               │
│ This allocation requires approval because:                      │
│ • Tier is "Production"                                          │
│ • Size > /26                                                    │
│                                                                 │
│ Approvers: @network-admins (2 required)                         │
│                                                    [Submit →]   │
└─────────────────────────────────────────────────────────────────┘

Step 3: Confirmation
┌─────────────────────────────────────────────────────────────────┐
│ ✅ REQUEST SUBMITTED                                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Request ID: REQ-2024-0142                                       │
│ Status: Pending Approval                                        │
│                                                                 │
│ Block:    10.0.9.0/24                                           │
│ Name:     payment-service-subnet                                │
│ Account:  aws:123456789012                                      │
│                                                                 │
│ Approvers notified:                                             │
│ • @alice (network-admin) - pending                              │
│ • @bob (network-admin) - pending                                │
│                                                                 │
│ You'll receive an email when approved.                          │
│                                                                 │
│ [View Request] [Back to Pools]                                  │
└─────────────────────────────────────────────────────────────────┘
```

---
