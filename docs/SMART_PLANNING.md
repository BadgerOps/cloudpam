# CloudPAM Smart Planning Architecture

## Overview

Smart Planning is CloudPAM's intelligent network planning system that analyzes existing infrastructure, understands user intent, and generates optimized IP address allocation recommendations. It combines rule-based analysis with optional AI/LLM integration for natural language planning.

## Core Components

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Smart Planning Engine                              │
│  ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────────────────┐│
│  │   Discovery     │  │    Analysis      │  │    Recommendation           ││
│  │   Aggregator    │──│    Engine        │──│    Generator                ││
│  └─────────────────┘  └──────────────────┘  └─────────────────────────────┘│
│          │                    │                         │                   │
│          ▼                    ▼                         ▼                   │
│  ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────────────────┐│
│  │ • Cloud APIs    │  │ • Gap Analysis   │  │ • Schema Templates          ││
│  │ • CSV Import    │  │ • Fragmentation  │  │ • Allocation Plans          ││
│  │ • Manual Entry  │  │ • Utilization    │  │ • Growth Projections        ││
│  │ • Network Scan  │  │ • Compliance     │  │ • Migration Paths           ││
│  └─────────────────┘  └──────────────────┘  └─────────────────────────────┘│
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                        AI Planning Assistant (Optional)                  ││
│  │   • Natural Language Intent → Structured Requirements                    ││
│  │   • Pattern Recognition → Best Practice Recommendations                  ││
│  │   • What-If Analysis → Risk Assessment                                   ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
```

## 1. Discovery Aggregator

### 1.1 Data Sources

| Source | Method | Data Captured |
|--------|--------|---------------|
| **AWS** | EC2/VPC API via Collector | VPCs, Subnets, ENIs, EIPs, CIDR blocks |
| **GCP** | Compute API via Collector | Networks, Subnetworks, IP addresses |
| **Azure** | ARM API via Collector | VNets, Subnets, NICs, Public IPs |
| **On-Prem** | SNMP/SSH Scanner | Switch VLANs, Router interfaces, DHCP scopes |
| **DNS** | Zone Transfer/API | Forward/reverse zones, A/AAAA records |
| **CSV/Excel** | File Import | Legacy IPAM data, spreadsheet inventories |
| **CMDB** | API Integration | ServiceNow, Device42, etc. |

### 1.2 Import Formats

**CSV Import Schema:**
```csv
cidr,name,environment,region,type,notes
10.0.0.0/16,Production VPC,production,us-east-1,vpc,Main production network
10.0.1.0/24,Web Tier,production,us-east-1,subnet,Public-facing web servers
```

**Excel Import:** Multi-sheet support with header mapping wizard and data validation preview.

## 2. Analysis Engine

### 2.1 Gap Analysis

Identifies unused address space within allocated ranges:

```
Parent: 10.0.0.0/16 (65,536 addresses)
├── Allocated: 10.0.0.0/24 (256) - Web Tier
├── Allocated: 10.0.1.0/24 (256) - App Tier
├── [AVAILABLE] 10.0.3.0/24 (256 addresses)
├── [AVAILABLE] 10.0.4.0/22 (1,024 addresses)
└── [AVAILABLE] 10.0.8.0/21 (2,048 addresses)

Utilization: 1.2% (768 / 65,536)
```

### 2.2 Fragmentation Analysis

| Type | Description | Example |
|------|-------------|---------|
| **Scattered** | Small allocations spread across large space | /28s scattered in a /16 |
| **Oversized** | Allocation much larger than actual usage | /22 with 10 hosts |
| **Undersized** | Allocation approaching capacity | /24 at 90% utilization |
| **Misaligned** | CIDR boundaries don't follow best practices | 10.0.3.0/23 instead of 10.0.2.0/23 |

### 2.3 Compliance Analysis

Configurable rules including: minimum/maximum subnet sizes, reserved IP range protection, RFC1918 enforcement, environment isolation, and naming conventions.

### 2.4 Growth Projection

Predicts future address needs using linear regression, seasonal adjustment, and event-based factors.

## 3. Recommendation Generator

### 3.1 Recommendation Types

| Type | Description | Example |
|------|-------------|---------|
| `allocation` | Recommends specific CIDR | "Allocate 10.0.4.0/22 for new workload" |
| `consolidation` | Suggests merging ranges | "Merge fragmented /25s into /24" |
| `resize` | Recommends resizing | "Expand staging /24 to /23" |
| `reclaim` | Identifies reclaimable space | "10.0.5.0/24 has no active hosts" |
| `compliance` | Fixes compliance issues | "Rename pool to follow convention" |

### 3.2 Intelligent Allocation Algorithm

1. **Find candidate blocks** matching size requirements
2. **Score each candidate** based on contiguity, alignment, and growth runway
3. **Apply constraints** (region, environment isolation, etc.)
4. **Rank and return** top suggestions with rationale

## 4. AI Planning Assistant

### 4.1 LLM Integration Options

| Provider | Configuration | Use Case |
|----------|--------------|----------|
| **OpenAI** | API key + model selection | Cloud-hosted, GPT-4 quality |
| **Anthropic** | API key + model selection | Cloud-hosted, Claude quality |
| **Azure OpenAI** | Endpoint + deployment | Enterprise compliance |
| **Ollama** | Local endpoint | Air-gapped, self-hosted |
| **vLLM** | OpenAI-compatible endpoint | Self-hosted, GPU acceleration |

### 4.2 Context Injection

The AI assistant receives structured context about your infrastructure including pools, available space, compliance rules, recent allocations, and organization preferences.

### 4.3 Example Conversation

**Kubernetes Cluster Planning**

```
User: "I need to plan networking for a new Kubernetes cluster in production.
       We'll have about 100 nodes and use Calico for pod networking."
Assistant: "Based on your current production infrastructure (10.1.0.0/16),
           I recommend:

           **Node Network**: 10.1.8.0/22 (1,024 addresses)
           **Pod CIDR**: 10.1.64.0/18 (16,384 addresses)
           **Service CIDR**: 10.1.128.0/20 (4,096 addresses)

           Shall I create a schema plan with these allocations?"

User: "Yes, and also reserve space for a DR cluster in us-west-2"

Assistant: "I will extend the plan to include DR in us-west-2:

           prod-us-east-1 (10.1.0.0/16)
           +-- k8s-cluster-01 [NEW]
               +-- nodes     10.1.8.0/22
               +-- pods      10.1.64.0/18
               +-- services  10.1.128.0/20

           prod-us-west-2 (10.2.0.0/16) [NEW REGION]
           +-- k8s-cluster-dr [NEW]
               +-- nodes     10.2.8.0/22
               +-- pods      10.2.64.0/18
               +-- services  10.2.128.0/20

           Ready to apply?"
```

## 5. API Reference

Smart Planning endpoints are documented in `openapi-smart-planning.yaml`:

| Category | Endpoints |
|----------|----------|
| **Discovery** | POST /planning/import, GET /planning/discovered, POST /planning/sync |
| **Analysis** | POST /planning/analyze, POST /planning/analyze/gaps, /fragmentation, /compliance, /growth |
| **Recommendations** | GET /planning/recommendations, POST .../apply, POST .../dismiss |
| **AI Planning** | POST /planning/ai/conversations, GET .../messages, POST .../plans/{id}/apply |
| **Schema Wizard** | GET /planning/schema/templates, POST /planning/schema/generate, /validate, /apply |
| **LLM Config** | GET/PUT /settings/llm, POST /settings/llm/test, GET/PUT /settings/llm/prompts |

See [openapi-smart-planning.yaml](openapi-smart-planning.yaml) for full specification.

## 6. Go Interfaces

Service interfaces are defined in `internal/planning/interfaces.go`. See that file for:
- DiscoveryService
- AnalysisService
- RecommendationService
- AIPlanningService
- SchemaWizardService
- LLMProvider
