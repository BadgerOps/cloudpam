# CloudPAM API Examples

Practical examples for common API operations.

## Authentication

### Using API Key

```bash
# All requests require authentication
curl -X GET "https://cloudpam.example.com/api/v1/pools" \
  -H "X-API-Key: cpam_v1_abc12345_x7k9mN2pQr5tVw8yZa4bCd6eF"
```

### Using Bearer Token (from OAuth flow)

```bash
curl -X GET "https://cloudpam.example.com/api/v1/pools" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiIs..."
```

---

## Pool Management

### List All Pools (Flat)

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/pools?limit=10" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "Global Address Space",
      "description": "Root allocation for all networks",
      "cidr": "10.0.0.0/8",
      "type": "supernet",
      "parent_id": null,
      "tags": {
        "managed_by": "cloudpam"
      },
      "created_at": "2024-01-10T08:00:00Z",
      "updated_at": "2024-01-10T08:00:00Z"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "name": "US East Region",
      "cidr": "10.1.0.0/16",
      "type": "region",
      "parent_id": "550e8400-e29b-41d4-a716-446655440001",
      "tags": {
        "region": "us-east-1"
      },
      "created_at": "2024-01-10T08:05:00Z",
      "updated_at": "2024-01-10T08:05:00Z"
    }
  ],
  "meta": {
    "total": 47,
    "limit": 10,
    "has_more": true,
    "next_cursor": "eyJpZCI6IjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAxMCJ9"
  }
}
```

### Get Pool Tree (Hierarchical)

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/pools/tree" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "roots": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "Global Address Space",
      "cidr": "10.0.0.0/8",
      "type": "supernet",
      "utilization": {
        "total_addresses": 16777216,
        "allocated_addresses": 131072,
        "available_addresses": 16646144,
        "utilization_percent": 0.78,
        "child_count": 2
      },
      "children": [
        {
          "id": "550e8400-e29b-41d4-a716-446655440002",
          "name": "US East Region",
          "cidr": "10.1.0.0/16",
          "type": "region",
          "utilization": {
            "total_addresses": 65536,
            "allocated_addresses": 1024,
            "utilization_percent": 1.56,
            "child_count": 3
          },
          "children": [
            {
              "id": "550e8400-e29b-41d4-a716-446655440010",
              "name": "US East Production",
              "cidr": "10.1.0.0/20",
              "type": "environment",
              "tags": { "environment": "production" },
              "children": []
            },
            {
              "id": "550e8400-e29b-41d4-a716-446655440011",
              "name": "US East Staging",
              "cidr": "10.1.16.0/20",
              "type": "environment",
              "tags": { "environment": "staging" },
              "children": []
            }
          ]
        },
        {
          "id": "550e8400-e29b-41d4-a716-446655440003",
          "name": "US West Region",
          "cidr": "10.2.0.0/16",
          "type": "region",
          "children": []
        }
      ]
    }
  ],
  "total_pools": 47,
  "max_depth": 5
}
```

### Create Pool

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/pools" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "EU West Production VPC",
    "description": "Production workloads in eu-west-1",
    "cidr": "10.3.0.0/20",
    "type": "vpc",
    "parent_id": "550e8400-e29b-41d4-a716-446655440003",
    "tags": {
      "environment": "production",
      "region": "eu-west-1",
      "cost_center": "engineering"
    }
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440050",
  "name": "EU West Production VPC",
  "description": "Production workloads in eu-west-1",
  "cidr": "10.3.0.0/20",
  "type": "vpc",
  "parent_id": "550e8400-e29b-41d4-a716-446655440003",
  "tags": {
    "environment": "production",
    "region": "eu-west-1",
    "cost_center": "engineering"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:00Z",
  "created_by": "550e8400-e29b-41d4-a716-446655440099"
}
```

**Error Response (409 Conflict):**
```json
{
  "error": {
    "code": "CIDR_CONFLICT",
    "message": "CIDR 10.3.0.0/20 overlaps with existing pool",
    "conflicting_pools": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440025",
        "name": "EU West Staging",
        "cidr": "10.3.0.0/22"
      }
    ]
  }
}
```

### Allocate from Pool

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/pools/550e8400-e29b-41d4-a716-446655440050/allocate" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "app-tier-subnet",
    "prefix_length": 24,
    "type": "subnet",
    "strategy": "first_fit",
    "tags": {
      "tier": "application",
      "auto_allocated": "true"
    }
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440051",
  "name": "app-tier-subnet",
  "cidr": "10.3.0.0/24",
  "type": "subnet",
  "parent_id": "550e8400-e29b-41d4-a716-446655440050",
  "tags": {
    "tier": "application",
    "auto_allocated": "true"
  },
  "created_at": "2024-01-15T10:35:00Z",
  "updated_at": "2024-01-15T10:35:00Z"
}
```

### Validate CIDR Before Creation

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/pools/validate-cidr" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "cidr": "10.1.0.0/24",
    "parent_id": "550e8400-e29b-41d4-a716-446655440002"
  }'
```

**Response:**
```json
{
  "valid": true,
  "cidr": "10.1.0.0/24",
  "normalized_cidr": "10.1.0.0/24",
  "network_address": "10.1.0.0",
  "broadcast_address": "10.1.0.255",
  "total_addresses": 256,
  "usable_addresses": 254,
  "prefix_length": 24,
  "ip_version": 4,
  "fits_in_parent": true,
  "conflicts": [
    {
      "pool_id": "550e8400-e29b-41d4-a716-446655440010",
      "pool_name": "US East Production",
      "cidr": "10.1.0.0/20",
      "overlap_type": "contained_by"
    }
  ],
  "warnings": [
    "CIDR is contained within existing pool 'US East Production' (10.1.0.0/20)"
  ]
}
```

---

## Schema Planning

### List Available Templates

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/schema-templates?category=enterprise" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "data": [
    {
      "id": "template-enterprise-multi-region",
      "name": "Enterprise Multi-Region",
      "description": "Hierarchical structure for enterprises with multiple regions and environments",
      "category": "enterprise",
      "is_builtin": true,
      "structure": {
        "levels": [
          { "name": "region", "prefix_length": 16, "naming_pattern": "{region}" },
          { "name": "environment", "prefix_length": 20, "naming_pattern": "{region}-{env}" },
          { "name": "vpc", "prefix_length": 22, "naming_pattern": "{region}-{env}-vpc{n}" },
          { "name": "subnet", "prefix_length": 24, "naming_pattern": "{region}-{env}-{tier}" }
        ]
      },
      "parameters": [
        {
          "name": "regions",
          "type": "multi_select",
          "label": "Regions",
          "required": true,
          "options": [
            { "value": "us-east-1", "label": "US East (N. Virginia)" },
            { "value": "us-west-2", "label": "US West (Oregon)" },
            { "value": "eu-west-1", "label": "EU (Ireland)" }
          ]
        },
        {
          "name": "environments",
          "type": "multi_select",
          "label": "Environments",
          "required": true,
          "default": "production,staging",
          "options": [
            { "value": "production", "label": "Production" },
            { "value": "staging", "label": "Staging" },
            { "value": "development", "label": "Development" }
          ]
        }
      ]
    }
  ]
}
```

### Generate Schema Plan

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/schema-plans/generate" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "template_id": "template-enterprise-multi-region",
    "root_cidr": "10.0.0.0/8",
    "parameters": {
      "regions": ["us-east-1", "us-west-2"],
      "environments": ["production", "staging"],
      "vpcs_per_env": 2,
      "subnets_per_vpc": 3
    }
  }'
```

**Response:**
```json
{
  "id": null,
  "name": "Generated Plan",
  "status": "draft",
  "template_id": "template-enterprise-multi-region",
  "root_cidr": "10.0.0.0/8",
  "parameters": {
    "regions": ["us-east-1", "us-west-2"],
    "environments": ["production", "staging"]
  },
  "pools": [
    {
      "temp_id": "tmp-1",
      "name": "us-east-1",
      "cidr": "10.0.0.0/16",
      "type": "region",
      "parent_temp_id": null,
      "tags": { "region": "us-east-1" }
    },
    {
      "temp_id": "tmp-2",
      "name": "us-east-1-production",
      "cidr": "10.0.0.0/20",
      "type": "environment",
      "parent_temp_id": "tmp-1",
      "tags": { "region": "us-east-1", "environment": "production" }
    },
    {
      "temp_id": "tmp-3",
      "name": "us-east-1-production-vpc1",
      "cidr": "10.0.0.0/22",
      "type": "vpc",
      "parent_temp_id": "tmp-2",
      "tags": { "region": "us-east-1", "environment": "production", "vpc": "1" }
    },
    {
      "temp_id": "tmp-4",
      "name": "us-east-1-production-vpc1-app",
      "cidr": "10.0.0.0/24",
      "type": "subnet",
      "parent_temp_id": "tmp-3",
      "tags": { "tier": "application" }
    },
    {
      "temp_id": "tmp-5",
      "name": "us-east-1-production-vpc1-data",
      "cidr": "10.0.1.0/24",
      "type": "subnet",
      "parent_temp_id": "tmp-3",
      "tags": { "tier": "data" }
    },
    {
      "temp_id": "tmp-6",
      "name": "us-east-1-production-vpc1-web",
      "cidr": "10.0.2.0/24",
      "type": "subnet",
      "parent_temp_id": "tmp-3",
      "tags": { "tier": "web" }
    }
  ]
}
```

### Save Schema Plan

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/schema-plans" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Q1 2024 Network Expansion",
    "description": "Adding US West and EU regions for global expansion",
    "template_id": "template-enterprise-multi-region",
    "root_cidr": "10.0.0.0/8",
    "pools": [
      {
        "temp_id": "tmp-1",
        "name": "us-west-2",
        "cidr": "10.2.0.0/16",
        "type": "region",
        "parent_temp_id": null,
        "tags": { "region": "us-west-2" }
      }
    ]
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440100",
  "name": "Q1 2024 Network Expansion",
  "description": "Adding US West and EU regions for global expansion",
  "status": "draft",
  "template_id": "template-enterprise-multi-region",
  "root_cidr": "10.0.0.0/8",
  "pools": [...],
  "created_at": "2024-01-15T11:00:00Z",
  "updated_at": "2024-01-15T11:00:00Z",
  "created_by": "550e8400-e29b-41d4-a716-446655440099"
}
```

### Preview Plan Application

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/schema-plans/550e8400-e29b-41d4-a716-446655440100/preview" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "pools": [
    {
      "temp_id": "tmp-1",
      "name": "us-west-2",
      "cidr": "10.2.0.0/16",
      "type": "region",
      "status": "will_create"
    }
  ],
  "total_pools": 12,
  "conflicts": [],
  "warnings": [
    "Pool 'us-west-2' (10.2.0.0/16) will use 25% of remaining address space"
  ]
}
```

### Apply Schema Plan

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/schema-plans/550e8400-e29b-41d4-a716-446655440100/apply" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "dry_run": false }'
```

**Response:**
```json
{
  "success": true,
  "pools_created": 12,
  "pools": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440200",
      "name": "us-west-2",
      "cidr": "10.2.0.0/16",
      "type": "region"
    }
  ],
  "errors": []
}
```

---

## Cloud Accounts

### Add AWS Account

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/accounts" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production AWS",
    "provider": "aws",
    "provider_account_id": "123456789012",
    "auth_type": "iam_role",
    "auth_config": {
      "role_arn": "arn:aws:iam::123456789012:role/CloudPAMDiscovery",
      "external_id": "cloudpam-prod-abc123"
    },
    "regions": ["us-east-1", "us-west-2", "eu-west-1"],
    "auto_sync": true,
    "sync_interval_minutes": 30
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440300",
  "name": "Production AWS",
  "provider": "aws",
  "status": "connected",
  "provider_account_id": "123456789012",
  "auth_type": "iam_role",
  "auth_config": {
    "role_arn": "arn:aws:iam::123456789012:role/CloudPAMDiscovery"
  },
  "regions": ["us-east-1", "us-west-2", "eu-west-1"],
  "last_sync": null,
  "resource_counts": {
    "vpcs": 0,
    "subnets": 0,
    "network_interfaces": 0,
    "elastic_ips": 0
  },
  "created_at": "2024-01-15T12:00:00Z",
  "updated_at": "2024-01-15T12:00:00Z"
}
```

### Add GCP Account

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/accounts" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production GCP",
    "provider": "gcp",
    "provider_account_id": "my-gcp-project-123",
    "auth_type": "workload_identity",
    "auth_config": {
      "service_account_email": "cloudpam@my-gcp-project-123.iam.gserviceaccount.com",
      "project_id": "my-gcp-project-123"
    },
    "regions": ["us-central1", "us-east1", "europe-west1"],
    "auto_sync": true
  }'
```

### Test Account Connection

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/accounts/550e8400-e29b-41d4-a716-446655440300/test" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "success": true,
  "latency_ms": 245,
  "permissions_valid": true,
  "missing_permissions": [],
  "error": null
}
```

**Error Response:**
```json
{
  "success": false,
  "latency_ms": 1523,
  "permissions_valid": false,
  "missing_permissions": [
    "ec2:DescribeSubnets",
    "ec2:DescribeNetworkInterfaces"
  ],
  "error": "Access denied for ec2:DescribeSubnets"
}
```

### Trigger Manual Sync

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/accounts/550e8400-e29b-41d4-a716-446655440300/sync" \
  -H "X-API-Key: $API_KEY"
```

**Response (202 Accepted):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440400",
  "account_id": "550e8400-e29b-41d4-a716-446655440300",
  "status": "pending",
  "started_at": "2024-01-15T12:30:00Z",
  "completed_at": null
}
```

---

## Discovery

### List Discovered Resources

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/discovery/resources?account_id=550e8400-e29b-41d4-a716-446655440300&status=untracked&limit=20" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440500",
      "account_id": "550e8400-e29b-41d4-a716-446655440300",
      "provider": "aws",
      "resource_type": "vpc",
      "resource_id": "vpc-0abc123def456789",
      "name": "production-vpc",
      "region": "us-east-1",
      "cidr": "172.16.0.0/16",
      "status": "untracked",
      "linked_pool_id": null,
      "metadata": {
        "aws_vpc_id": "vpc-0abc123def456789",
        "is_default": false,
        "state": "available",
        "tags": {
          "Name": "production-vpc",
          "Environment": "production"
        }
      },
      "first_seen": "2024-01-15T12:35:00Z",
      "last_seen": "2024-01-15T12:35:00Z"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440501",
      "account_id": "550e8400-e29b-41d4-a716-446655440300",
      "provider": "aws",
      "resource_type": "subnet",
      "resource_id": "subnet-0abc123def456789",
      "name": "production-app-subnet",
      "region": "us-east-1",
      "cidr": "172.16.1.0/24",
      "status": "untracked",
      "linked_pool_id": null,
      "metadata": {
        "aws_subnet_id": "subnet-0abc123def456789",
        "vpc_id": "vpc-0abc123def456789",
        "availability_zone": "us-east-1a",
        "available_ip_count": 251
      },
      "first_seen": "2024-01-15T12:35:00Z",
      "last_seen": "2024-01-15T12:35:00Z"
    }
  ],
  "meta": {
    "total": 47,
    "limit": 20,
    "has_more": true,
    "next_cursor": "eyJpZCI6IjU1MGU4NDAw..."
  }
}
```

### Link Resource to Pool

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/discovery/resources/550e8400-e29b-41d4-a716-446655440500/link" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "pool_id": "550e8400-e29b-41d4-a716-446655440050"
  }'
```

**Response:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440500",
  "status": "tracked",
  "linked_pool_id": "550e8400-e29b-41d4-a716-446655440050"
}
```

### Get Drift Report

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/discovery/drift?severity=warning" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "generated_at": "2024-01-15T13:00:00Z",
  "summary": {
    "total_resources": 156,
    "tracked": 142,
    "untracked": 8,
    "conflicts": 4,
    "orphaned": 2
  },
  "items": [
    {
      "resource": {
        "id": "550e8400-e29b-41d4-a716-446655440510",
        "resource_type": "subnet",
        "resource_id": "subnet-0xyz789",
        "name": "unknown-subnet",
        "cidr": "10.1.5.0/24"
      },
      "drift_type": "untracked",
      "severity": "warning",
      "expected": null,
      "actual": {
        "cidr": "10.1.5.0/24",
        "region": "us-east-1"
      },
      "recommendation": "This subnet exists in AWS but is not managed by CloudPAM. Consider linking it to an existing pool or creating a new pool."
    },
    {
      "resource": {
        "id": "550e8400-e29b-41d4-a716-446655440511",
        "resource_type": "subnet",
        "resource_id": "subnet-0conflict",
        "name": "conflicting-subnet",
        "cidr": "10.1.0.0/24"
      },
      "drift_type": "conflict",
      "severity": "critical",
      "expected": {
        "pool_name": "us-east-production-app",
        "cidr": "10.1.0.0/24"
      },
      "actual": {
        "cidr": "10.1.0.0/25",
        "region": "us-east-1"
      },
      "recommendation": "CIDR mismatch: Pool defines 10.1.0.0/24 but AWS shows 10.1.0.0/25. Update pool or investigate AWS configuration."
    }
  ]
}
```

---

## AI Planning

### Create Conversation

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/planning/ai/conversations" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "organization_id": "550e8400-e29b-41d4-a716-446655440000",
    "user_id": "550e8400-e29b-41d4-a716-446655440099",
    "title": "Kubernetes Cluster Planning"
  }'
```

**Response (201 Created):**
```json
{
  "id": "conv-550e8400-e29b-41d4-a716-446655441000",
  "organization_id": "550e8400-e29b-41d4-a716-446655440000",
  "user_id": "550e8400-e29b-41d4-a716-446655440099",
  "title": "Kubernetes Cluster Planning",
  "status": "active",
  "messages": [],
  "generated_plans": [],
  "context": {
    "available_pools": 12,
    "total_available_space": "10.0.0.0/8 - 67% available"
  },
  "created_at": "2024-01-15T15:00:00Z",
  "updated_at": "2024-01-15T15:00:00Z"
}
```

### Send Message (User Intent)

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/messages" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "I need to plan networking for a new Kubernetes cluster in production. We will have about 100 nodes and use Calico for pod networking. The cluster will be in us-east-1."
  }'
```

**Response (AI generates plan):**
```json
{
  "id": "msg-550e8400-e29b-41d4-a716-446655441001",
  "role": "assistant",
  "content": "Based on your current production infrastructure (10.1.0.0/16, region-first hierarchy), I recommend the following allocation for your Kubernetes cluster:\n\n**Node Network**: 10.1.8.0/22 (1,024 addresses)\n- Room for 100 nodes + growth headroom\n- Adjacent to existing production workloads\n\n**Pod CIDR**: 10.1.64.0/18 (16,384 addresses)\n- Calico default 256 pods per node = 25,600 pods max\n- A /18 provides good runway\n\n**Service CIDR**: 10.1.128.0/20 (4,096 addresses)\n- Kubernetes services typically need fewer IPs\n\nWould you like me to create this plan?",
  "generated_plan": {
    "id": "plan-550e8400-e29b-41d4-a716-446655441100",
    "name": "k8s-cluster-us-east-1",
    "status": "draft",
    "allocations": [
      {
        "name": "k8s-nodes",
        "cidr": "10.1.8.0/22",
        "type": "subnet",
        "purpose": "Kubernetes node network"
      },
      {
        "name": "k8s-pods",
        "cidr": "10.1.64.0/18",
        "type": "pod_cidr",
        "purpose": "Calico pod network"
      },
      {
        "name": "k8s-services",
        "cidr": "10.1.128.0/20",
        "type": "service_cidr",
        "purpose": "Kubernetes services"
      }
    ],
    "warnings": []
  },
  "ai_metadata": {
    "model": "claude-3-5-sonnet",
    "provider": "anthropic",
    "tokens_used": 1247
  },
  "created_at": "2024-01-15T15:01:00Z"
}
```

### List Conversation Plans

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/plans" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "plans": [
    {
      "id": "plan-550e8400-e29b-41d4-a716-446655441100",
      "name": "k8s-cluster-us-east-1",
      "status": "draft",
      "created_at": "2024-01-15T15:01:00Z",
      "allocation_count": 3,
      "total_addresses": 21504
    }
  ]
}
```

### Validate Plan Before Applying

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/plans/plan-550e8400-e29b-41d4-a716-446655441100/validate" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "valid": true,
  "warnings": [
    {
      "code": "LARGE_ALLOCATION",
      "message": "Pod CIDR 10.1.64.0/18 allocates 16,384 addresses. Consider if this is necessary.",
      "severity": "info"
    }
  ],
  "conflicts": []
}
```

### Apply Plan (Dry Run)

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/plans/plan-550e8400-e29b-41d4-a716-446655441100/apply" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "dry_run": true,
    "confirm": false
  }'
```

**Response (Dry Run Preview):**
```json
{
  "success": true,
  "dry_run": true,
  "pools_to_create": 3,
  "preview": [
    {
      "name": "k8s-nodes",
      "cidr": "10.1.8.0/22",
      "parent": "us-east-1-production",
      "action": "create"
    },
    {
      "name": "k8s-pods",
      "cidr": "10.1.64.0/18",
      "parent": "us-east-1-production",
      "action": "create"
    },
    {
      "name": "k8s-services",
      "cidr": "10.1.128.0/20",
      "parent": "us-east-1-production",
      "action": "create"
    }
  ],
  "summary": "This will create 3 pools with a total of 21,504 IP addresses."
}
```

### Apply Plan (Confirmed)

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/plans/plan-550e8400-e29b-41d4-a716-446655441100/apply" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "dry_run": false,
    "confirm": true
  }'
```

**Response:**
```json
{
  "success": true,
  "pools_created": 3,
  "summary": "Created 3 pools for Kubernetes cluster in us-east-1",
  "created_pools": [
    {
      "id": "550e8400-e29b-41d4-a716-446655442001",
      "name": "k8s-nodes",
      "cidr": "10.1.8.0/22"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655442002",
      "name": "k8s-pods",
      "cidr": "10.1.64.0/18"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655442003",
      "name": "k8s-services",
      "cidr": "10.1.128.0/20"
    }
  ]
}
```

### Export Plan as Terraform

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000/plans/plan-550e8400-e29b-41d4-a716-446655441100/export?format=terraform" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "format": "terraform",
  "content": "# Generated by CloudPAM AI Planning\n# Plan: k8s-cluster-us-east-1\n\nresource \"aws_subnet\" \"k8s_nodes\" {\n  vpc_id     = var.vpc_id\n  cidr_block = \"10.1.8.0/22\"\n\n  tags = {\n    Name        = \"k8s-nodes\"\n    Environment = \"production\"\n    ManagedBy   = \"cloudpam\"\n  }\n}\n\nresource \"aws_subnet\" \"k8s_pods\" {\n  vpc_id     = var.vpc_id\n  cidr_block = \"10.1.64.0/18\"\n\n  tags = {\n    Name        = \"k8s-pods\"\n    Environment = \"production\"\n    ManagedBy   = \"cloudpam\"\n  }\n}\n\nresource \"aws_subnet\" \"k8s_services\" {\n  vpc_id     = var.vpc_id\n  cidr_block = \"10.1.128.0/20\"\n\n  tags = {\n    Name        = \"k8s-services\"\n    Environment = \"production\"\n    ManagedBy   = \"cloudpam\"\n  }\n}\n"
}
```

### Update Conversation Title

**Request:**
```bash
curl -X PATCH "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "K8s Production Cluster - US East"
  }'
```

**Response:**
```json
{
  "id": "conv-550e8400-e29b-41d4-a716-446655441000",
  "title": "K8s Production Cluster - US East",
  "status": "active",
  "updated_at": "2024-01-15T15:10:00Z"
}
```

### Delete/Archive Conversation

**Request:**
```bash
curl -X DELETE "https://cloudpam.example.com/api/v1/planning/ai/conversations/conv-550e8400-e29b-41d4-a716-446655441000" \
  -H "X-API-Key: $API_KEY"
```

**Response:** `204 No Content`

---

## Users and Roles

### List Users

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/users" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440099",
      "email": "admin@example.com",
      "name": "Admin User",
      "status": "active",
      "role": {
        "id": "role-admin",
        "name": "Admin"
      },
      "teams": [
        { "id": "team-1", "name": "Platform Team", "role": "lead" }
      ],
      "last_login": "2024-01-15T09:00:00Z",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ],
  "meta": {
    "total": 15,
    "limit": 20,
    "has_more": false
  }
}
```

### Invite User

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/users" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "newuser@example.com",
    "name": "New User",
    "role_id": "role-editor",
    "team_ids": ["team-1", "team-2"]
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440150",
  "email": "newuser@example.com",
  "name": "New User",
  "status": "invited",
  "role": {
    "id": "role-editor",
    "name": "Editor"
  },
  "teams": [
    { "id": "team-1", "name": "Platform Team", "role": "member" },
    { "id": "team-2", "name": "US East Team", "role": "member" }
  ],
  "created_at": "2024-01-15T14:00:00Z"
}
```

### Create API Token

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/api-tokens" \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CI/CD Pipeline Token",
    "scopes": ["pools:read", "pools:write", "discovery:read"],
    "expires_in_days": 90
  }'
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440600",
  "name": "CI/CD Pipeline Token",
  "prefix": "abc12345",
  "token": "cpam_v1_abc12345_x7k9mN2pQr5tVw8yZa4bCd6eFgHiJkLmNoPqRsTuVwXyZ",
  "scopes": ["pools:read", "pools:write", "discovery:read"],
  "expires_at": "2024-04-15T14:00:00Z",
  "created_at": "2024-01-15T14:00:00Z",
  "created_by": "550e8400-e29b-41d4-a716-446655440099"
}
```

> **Note:** The full `token` value is only returned once at creation. Store it securely!

---

## Audit Log

### Query Audit Events

**Request:**
```bash
curl -X GET "https://cloudpam.example.com/api/v1/audit/events?resource_type=pool&action=create&from=2024-01-01T00:00:00Z&limit=10" \
  -H "X-API-Key: $API_KEY"
```

**Response:**
```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440700",
      "timestamp": "2024-01-15T10:30:00Z",
      "actor": {
        "id": "550e8400-e29b-41d4-a716-446655440099",
        "type": "user",
        "name": "Admin User",
        "email": "admin@example.com",
        "ip_address": "192.168.1.100"
      },
      "action": "create",
      "resource_type": "pool",
      "resource_id": "550e8400-e29b-41d4-a716-446655440050",
      "resource_name": "EU West Production VPC",
      "changes": {
        "before": null,
        "after": {
          "name": "EU West Production VPC",
          "cidr": "10.3.0.0/20",
          "type": "vpc"
        }
      },
      "metadata": {
        "request_id": "req-abc123",
        "user_agent": "Mozilla/5.0..."
      }
    }
  ],
  "meta": {
    "total": 234,
    "limit": 10,
    "has_more": true,
    "next_cursor": "eyJ0cyI6IjIwMjQtMDEtMTVUMTA6MjU6MDBaIn0="
  }
}
```

### Export Audit Log

**Request:**
```bash
curl -X POST "https://cloudpam.example.com/api/v1/audit/export" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "csv",
    "from": "2024-01-01T00:00:00Z",
    "to": "2024-01-31T23:59:59Z",
    "filters": {
      "actions": ["create", "delete"],
      "resource_types": ["pool", "account"]
    }
  }'
```

**Response (CSV):**
```csv
timestamp,actor_email,action,resource_type,resource_id,resource_name,ip_address
2024-01-15T10:30:00Z,admin@example.com,create,pool,550e8400...,EU West Production VPC,192.168.1.100
2024-01-14T15:45:00Z,admin@example.com,create,account,550e8400...,Production AWS,192.168.1.100
```

---

## Error Handling

### Validation Error

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "fields": [
      {
        "field": "cidr",
        "message": "Invalid CIDR notation",
        "code": "INVALID_FORMAT"
      },
      {
        "field": "name",
        "message": "Name is required",
        "code": "REQUIRED"
      }
    ]
  }
}
```

### Authentication Error

```json
{
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Invalid or expired API key"
  }
}
```

### Permission Error

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "You do not have permission to delete pools",
    "details": {
      "required_permission": "pools:delete",
      "user_permissions": ["pools:read", "pools:write"]
    }
  }
}
```

### Rate Limit Error

```json
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Too many requests",
    "details": {
      "limit": 100,
      "reset_at": "2024-01-15T14:01:00Z"
    }
  }
}
```

---

## SDK Examples

### Go Client

```go
package main

import (
    "context"
    "fmt"
    "github.com/badgerops/cloudpam-go"
)

func main() {
    client := cloudpam.NewClient(
        cloudpam.WithAPIKey("cpam_v1_abc12345_..."),
        cloudpam.WithBaseURL("https://cloudpam.example.com"),
    )

    // List pools
    pools, err := client.Pools.List(context.Background(), &cloudpam.PoolListOptions{
        Type:   cloudpam.PoolTypeVPC,
        Limit:  20,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, pool := range pools.Data {
        fmt.Printf("Pool: %s (%s)\n", pool.Name, pool.CIDR)
    }

    // Create pool
    newPool, err := client.Pools.Create(context.Background(), &cloudpam.CreatePoolRequest{
        Name:     "New Subnet",
        CIDR:     "10.5.0.0/24",
        Type:     cloudpam.PoolTypeSubnet,
        ParentID: "parent-pool-id",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created pool: %s\n", newPool.ID)
}
```

### Python Client

```python
from cloudpam import CloudPAMClient

client = CloudPAMClient(
    api_key="cpam_v1_abc12345_...",
    base_url="https://cloudpam.example.com"
)

# List pools
pools = client.pools.list(type="vpc", limit=20)
for pool in pools.data:
    print(f"Pool: {pool.name} ({pool.cidr})")

# Create pool
new_pool = client.pools.create(
    name="New Subnet",
    cidr="10.5.0.0/24",
    type="subnet",
    parent_id="parent-pool-id"
)
print(f"Created pool: {new_pool.id}")

# Allocate from pool
allocated = client.pools.allocate(
    pool_id="parent-pool-id",
    prefix_length=26,
    name="web-tier",
    tags={"tier": "web"}
)
print(f"Allocated: {allocated.cidr}")
```

### Terraform Provider

```hcl
terraform {
  required_providers {
    cloudpam = {
      source  = "badgerops/cloudpam"
      version = "~> 1.0"
    }
  }
}

provider "cloudpam" {
  api_key  = var.cloudpam_api_key
  base_url = "https://cloudpam.example.com"
}

# Allocate a new subnet from a pool
resource "cloudpam_allocation" "app_subnet" {
  pool_id       = "parent-pool-id"
  name          = "app-subnet-${var.environment}"
  prefix_length = 24
  type          = "subnet"

  tags = {
    environment = var.environment
    tier        = "application"
  }
}

# Use the allocated CIDR in AWS
resource "aws_subnet" "app" {
  vpc_id     = aws_vpc.main.id
  cidr_block = cloudpam_allocation.app_subnet.cidr

  tags = {
    Name = cloudpam_allocation.app_subnet.name
  }
}
```
