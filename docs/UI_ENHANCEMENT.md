# CloudPAM UI Enhancement Requirements

## Document Status
**Stage:** Historical discovery notes
**Last Updated:** 2026-01-27

This is early UI research, not current UI status. The running app is a sidebar-based React SPA. Phase 6 (Graphēon-style shell and shared primitives) is not started. Operator-facing UI gaps (analysis page, account edit, search deep-link, and others) are listed in [CURRENT_STATUS.md](CURRENT_STATUS.md).

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

**Q: Mockup feedback - utilization display?**
> Create HTML mockups for each option to evaluate visually.

**Q: Mockup feedback - allocation flow?**
> Expand on the allocation request flow with more detail.

**Q: Search interface details?**
> Pin for now - note to flesh out later.

**Q: Performance expectations?**
> - Current scale: Hundreds of pools
> - Future scale: Could expand significantly, especially with individual IPs
> - Acceptable latency: 1-2 seconds is fine
> - Note: Need to plan for performance as scale increases

---

## UI Mockups

### HTML Mockups

Interactive HTML mockups are available in the `docs/mockups/` directory:

| Mockup | File | Description |
|--------|------|-------------|
| **Utilization Display** | `utilization-options.html` | All 5 utilization display options (A-E) with pros/cons |
| **Global Search** | `search-interface.html` | Omnisearch with filters, CIDR/IP/name lookup |
| **Allocation Flow** | `allocation-flow.html` | Self-service allocation with RBAC and approval workflow |
| **Pool Detail** | `pool-detail-view.html` | Combined utilization bar, block map, and subnet table |
| **Drift & Reconciliation** | `drift-reconciliation.html` | Compare IPAM vs cloud state, resolve discrepancies |
| **Export & Reports** | `export-reports.html` | Data export, report templates, scheduled reports |

Open these files in a browser to see the interactive mockups.

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

### Expanded Allocation Flow: Request/Approve with RBAC Guardrails

#### Flow Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                    ALLOCATION REQUEST WORKFLOW                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────┐    ┌─────────────┐    ┌──────────────┐    ┌─────────┐ │
│  │ SELECT  │───▶│   DETAILS   │───▶│   APPROVAL   │───▶│ ACTIVE  │ │
│  │  BLOCK  │    │  & SUBMIT   │    │   (if req)   │    │         │ │
│  └─────────┘    └─────────────┘    └──────────────┘    └─────────┘ │
│       │                                   │                  │      │
│       ▼                                   ▼                  ▼      │
│  Guardrails:                        Approval Rules:    Provisioned: │
│  • Allowed pools                    • Tier = prod      • Pool created│
│  • Max size per tier                • Size > /26       • Audit logged│
│  • Account scope                    • Cross-account    • Notified    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

#### RBAC Guardrails (Pre-Request)

Before a user can even start a request, the system enforces guardrails:

| Guardrail | Description | Example |
|-----------|-------------|---------|
| **Allowed Parent Pools** | Users can only allocate from pools they have access to | Team "Payments" can only allocate from `payments-vpc` |
| **Account Scope** | Users can only allocate to accounts they're authorized for | User can only target `aws:prod-123` or `aws:dev-456` |
| **Max Subnet Size** | Limit maximum allocation size by tier | Dev: max /24, Prod: max /22 |
| **Tier Restrictions** | Some users can't request production at all | Junior devs can only request dev/staging |
| **Region Restrictions** | Limit allocations to approved regions | Only us-east-1 and eu-west-1 allowed |

```
┌─────────────────────────────────────────────────────────────────┐
│ REQUEST NEW SUBNET                                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Parent Pool:    [Select a pool...                        ▼]    │
│                 ┌────────────────────────────────────────┐     │
│                 │ ✓ payments-vpc (10.0.0.0/16)           │     │
│                 │ ✓ payments-dev (10.1.0.0/16)           │     │
│                 │ ✗ core-infra (no access)        🔒     │     │
│                 │ ✗ security-vpc (no access)      🔒     │     │
│                 └────────────────────────────────────────┘     │
│                                                                 │
│ ℹ️  You have access to 2 of 4 pools based on your team         │
│     membership (payments-team).                                 │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Approval Rules Engine

Approval requirements are configurable per organization:

```yaml
# Example approval rules configuration
approval_rules:
  - name: "Production requires network team approval"
    conditions:
      - field: tier
        operator: equals
        value: production
    approvers:
      - group: network-admins
        required: 1

  - name: "Large subnets require manager approval"
    conditions:
      - field: prefix_length
        operator: less_than
        value: 26  # /25 or larger
    approvers:
      - group: network-admins
        required: 1
      - group: cost-approvers
        required: 1

  - name: "Cross-account requires security review"
    conditions:
      - field: crosses_account_boundary
        operator: equals
        value: true
    approvers:
      - group: security-team
        required: 1

  # Auto-approve rules
  - name: "Dev allocations auto-approved"
    conditions:
      - field: tier
        operator: equals
        value: development
      - field: prefix_length
        operator: greater_than_or_equal
        value: 26  # /26 or smaller
    auto_approve: true
```

#### Request States

```
┌──────────┐     ┌───────────┐     ┌──────────┐     ┌────────┐
│  DRAFT   │────▶│  PENDING  │────▶│ APPROVED │────▶│ ACTIVE │
└──────────┘     └───────────┘     └──────────┘     └────────┘
     │                │                  │
     │                ▼                  │
     │          ┌──────────┐             │
     └─────────▶│ REJECTED │◀────────────┘
                └──────────┘
                     │
                     ▼
                ┌──────────┐
                │ EXPIRED  │ (after 7 days)
                └──────────┘
```

| State | Description | Actions Available |
|-------|-------------|-------------------|
| **Draft** | User started but hasn't submitted | Edit, Submit, Delete |
| **Pending** | Awaiting approval | Approve, Reject, Comment, Cancel |
| **Approved** | All approvals received | Provision, Cancel |
| **Rejected** | An approver rejected | Clone as new request, View feedback |
| **Active** | Subnet provisioned and in use | View details, Request decommission |
| **Expired** | Pending too long (7 days) | Clone as new request |

#### Approval Interface (For Approvers)

```
┌─────────────────────────────────────────────────────────────────┐
│ PENDING APPROVALS                                    [3 pending]│
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ REQ-2024-0142 • payment-service-subnet                      │ │
│ │ ───────────────────────────────────────────────────────────│ │
│ │ Requester: jane.doe@company.com (Payments Team)             │ │
│ │ Submitted: 2 hours ago                                      │ │
│ │                                                             │ │
│ │ Block:     10.0.9.0/24 (254 hosts)                         │ │
│ │ Parent:    prod-vpc-primary                                 │ │
│ │ Account:   aws:123456789012 (prod)                         │ │
│ │ Tier:      Production                                       │ │
│ │ Purpose:   Production payment processing API                │ │
│ │                                                             │ │
│ │ ⚠️  Approval required because:                              │ │
│ │    • Tier is Production                                     │ │
│ │    • Size > /26                                             │ │
│ │                                                             │ │
│ │ Approvals:                                                  │ │
│ │    ✓ @alice (network-admin) - Approved 1 hour ago          │ │
│ │    ○ @bob (network-admin) - Pending (you)                  │ │
│ │                                                             │ │
│ │ [View Full Details]                                         │ │
│ │                                                             │ │
│ │ Comment (optional):                                         │ │
│ │ ┌───────────────────────────────────────────────────────┐  │ │
│ │ │                                                       │  │ │
│ │ └───────────────────────────────────────────────────────┘  │ │
│ │                                                             │ │
│ │ [Approve ✓]  [Reject ✗]  [Request Changes]                 │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                 │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ REQ-2024-0143 • analytics-staging-subnet         [Expand ▶] │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                 │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ REQ-2024-0144 • ml-training-large               [Expand ▶]  │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Notifications

| Event | Notify | Channel |
|-------|--------|---------|
| Request submitted | Approvers | Email, Slack, In-app |
| Approval received | Requester, other approvers | Email, In-app |
| Request approved (all) | Requester | Email, Slack, In-app |
| Request rejected | Requester | Email, Slack, In-app |
| Request expiring (24h warning) | Requester, approvers | Email |
| Subnet provisioned | Requester, team | Email, Slack |

#### Edge Cases & Error States

```
┌─────────────────────────────────────────────────────────────────┐
│ ⚠️  CONFLICT DETECTED                                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ The block you selected (10.0.9.0/24) was allocated while       │
│ your request was pending.                                       │
│                                                                 │
│ Allocated by: REQ-2024-0140 (build-service-subnet)             │
│ Allocated at: 10 minutes ago                                    │
│                                                                 │
│ Options:                                                        │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ ○ Select next available block: 10.0.10.0/24                 │ │
│ │ ○ Choose a different block manually                         │ │
│ │ ○ Cancel this request                                       │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                 │
│ [Continue with 10.0.10.0/24]  [Choose Manually]  [Cancel]      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Other edge cases to handle:**

| Scenario | Handling |
|----------|----------|
| Block allocated during approval | Offer next available or re-select |
| Approver no longer has permission | Skip to next approver, notify admin |
| Parent pool deleted | Reject request, notify requester |
| Account deactivated | Reject request, notify requester |
| Requester leaves company | Reassign to manager or cancel |
| All approvers unavailable | Escalate to admin, extend timeout |

#### Audit Trail

Every action is logged:

```
┌─────────────────────────────────────────────────────────────────┐
│ AUDIT LOG: REQ-2024-0142                                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 2024-01-15 14:32:01  jane.doe      Created request              │
│ 2024-01-15 14:32:01  system        Selected block 10.0.9.0/24   │
│ 2024-01-15 14:32:15  jane.doe      Submitted for approval       │
│ 2024-01-15 14:32:15  system        Notified @alice, @bob        │
│ 2024-01-15 15:15:42  alice         Approved                     │
│                                    Comment: "Looks good, valid  │
│                                    use case for payment team"   │
│ 2024-01-15 16:45:33  bob           Approved                     │
│ 2024-01-15 16:45:33  system        All approvals received       │
│ 2024-01-15 16:45:34  system        Provisioned pool             │
│ 2024-01-15 16:45:34  system        Notified jane.doe            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---
