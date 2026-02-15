# CloudPAM Observability Architecture

## Overview

CloudPAM implements a comprehensive observability stack covering structured logging, metrics collection, distributed tracing, and audit logging. The architecture follows cloud-native best practices with pluggable backends for enterprise integration.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              CloudPAM Application                                │
│  ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────────────────────┐ │
│  │   Structured    │  │   OpenTelemetry  │  │         Audit Events            │ │
│  │   Logging       │  │   Metrics/Traces │  │         (Database)              │ │
│  │   (slog/JSON)   │  │                  │  │                                 │ │
│  └────────┬────────┘  └────────┬─────────┘  └───────────────┬─────────────────┘ │
└───────────┼─────────────────────┼───────────────────────────┼───────────────────┘
            │                     │                           │
            ▼                     ▼                           ▼
┌───────────────────┐  ┌──────────────────┐       ┌─────────────────────┐
│   Vector Sidecar  │  │    Prometheus    │       │   Audit Log API     │
│   (Log Shipping)  │  │    Collector     │       │   (In-App Viewer)   │
└─────────┬─────────┘  └────────┬─────────┘       └─────────────────────┘
          │                     │
          ▼                     ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           Observability Backends                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │   Syslog    │  │   Splunk    │  │   AWS       │  │   GCP Cloud Logging     │ │
│  │             │  │   HEC       │  │   CloudWatch│  │                         │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────────┘ │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                              │
│  │  Prometheus │  │   Grafana   │  │   Jaeger    │                              │
│  │  (Metrics)  │  │   (Viz)     │  │  (Tracing)  │                              │
│  └─────────────┘  └─────────────┘  └─────────────┘                              │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## 1. Structured Logging

### 1.1 Framework: Go slog (Standard Library)

CloudPAM uses Go's native `slog` package (Go 1.21+) for structured logging:

| Feature | Benefit |
|---------|---------|
| Standard library | No external dependencies, future-proof |
| JSON output | Machine-parseable for log aggregation |
| Context-aware | Correlation IDs propagate through request lifecycle |
| Handler composition | Sampling, filtering, multi-output support |
| High performance | Comparable to zap with better maintainability |

### 1.2 Log Levels

| Level | Usage | Example |
|-------|-------|---------|
| `DEBUG` | Development troubleshooting | Variable values, SQL queries |
| `INFO` | Significant business events | Pool created, sync completed |
| `WARN` | Recoverable issues | Retry attempt, deprecated API |
| `ERROR` | Failures requiring attention | Auth failed, database error |

### 1.3 Log Format (JSON)

```json
{
  "timestamp": "2025-01-30T14:32:15.123Z",
  "severity": "INFO",
  "message": "Pool created successfully",
  "service": "cloudpam",
  "version": "1.0.0",
  "environment": "production",
  "correlation_id": "req_abc123def456",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "http.method": "POST",
  "http.path": "/api/v1/pools",
  "http.status": 201,
  "user.id": "usr_12345",
  "pool.id": "pool_67890",
  "duration_ms": 42.5
}
```

### 1.4 Correlation ID Propagation

Every request receives a unique correlation ID that flows through all components:

```
Client Request
     │
     ▼ X-Correlation-ID: req_abc123
┌─────────────────┐
│  HTTP Handler   │ ─── logs with correlation_id
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Service Layer  │ ─── logs with correlation_id
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Repository     │ ─── logs with correlation_id
└────────┬────────┘
         │
         ▼ Response includes X-Correlation-ID
     Client
```

## 2. Log Shipping with Vector

### 2.1 Why Vector?

| Aspect | Vector | Fluentd |
|--------|--------|---------|
| Language | Rust (memory-efficient) | Ruby (flexible) |
| Startup | <100ms | 1-2 seconds |
| Memory | 20-50 MB | 50-100 MB |
| Config | TOML (simple) | Ruby/JSON (complex) |
| Performance | Higher throughput | Proven, solid |

### 2.2 Deployment Pattern: Sidecar

CloudPAM deploys Vector as a sidecar container in Kubernetes:

```yaml
# Pod structure
containers:
  - name: cloudpam        # Main application
    volumeMounts:
      - name: logs
        mountPath: /var/log/cloudpam

  - name: vector          # Log shipping sidecar
    image: timberio/vector:0.34
    volumeMounts:
      - name: logs
        mountPath: /var/log/cloudpam
        readOnly: true
```

Benefits:
- Decouples log shipping from application
- Independent updates to shipping config
- Handles backpressure gracefully
- Supports multiple destinations simultaneously

### 2.3 Supported Destinations

| Destination | Use Case | Configuration |
|-------------|----------|---------------|
| **Syslog** | Enterprise log aggregation | TCP/UDP to syslog server |
| **Splunk** | SIEM integration | HTTP Event Collector (HEC) |
| **AWS CloudWatch** | AWS-native logging | IAM role authentication |
| **GCP Cloud Logging** | GCP-native logging | Service account |
| **Datadog** | SaaS observability | API key |
| **Elasticsearch** | Self-hosted search | Bulk API |

### 2.4 Vector Configuration

See `deploy/vector/vector.toml` for full configuration. Key sections:

```toml
# Collect logs from CloudPAM
[sources.app_logs]
type = "file"
includes = ["/var/log/cloudpam/*.log"]

# Parse JSON and enrich
[transforms.parse]
type = "remap"
inputs = ["app_logs"]
source = '''
. = parse_json!(.message)
.kubernetes.pod_name = get_env_var!("POD_NAME")
'''

# Ship to multiple destinations
[sinks.splunk]
type = "splunk_hec"
inputs = ["parse"]
endpoint = "${SPLUNK_HEC_URL}"
token = "${SPLUNK_HEC_TOKEN}"
```

## 3. Metrics with OpenTelemetry

### 3.1 Instrumentation Strategy

CloudPAM exports metrics via OpenTelemetry SDK to Prometheus:

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  CloudPAM App    │────▶│  OTLP Exporter   │────▶│   Prometheus     │
│  (OTel SDK)      │     │  :8889/metrics   │     │   Scraper        │
└──────────────────┘     └──────────────────┘     └──────────────────┘
                                                           │
                                                           ▼
                                                  ┌──────────────────┐
                                                  │     Grafana      │
                                                  │   Dashboards     │
                                                  └──────────────────┘
```

### 3.2 Metrics Catalog

#### HTTP Metrics (RED Method)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_request_duration_seconds` | Histogram | method, path, status | Request latency |
| `http_requests_total` | Counter | method, path, status | Request count |
| `http_request_size_bytes` | Histogram | method, path | Request body size |
| `http_response_size_bytes` | Histogram | method, path | Response body size |
| `http_connections_active` | Gauge | - | Active connections |

#### Database Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `db_query_duration_seconds` | Histogram | operation | Query latency |
| `db_queries_total` | Counter | operation, status | Query count |
| `db_connections_pool_size` | Gauge | - | Pool size |
| `db_connections_in_use` | Gauge | - | Connections in use |

#### Business Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cloudpam_pool_count` | Gauge | org_id, type | Total pools |
| `cloudpam_pool_utilization_percent` | Gauge | pool_id, type | Pool utilization |
| `cloudpam_allocation_duration_seconds` | Histogram | pool_id | Allocation latency |
| `cloudpam_allocation_failures_total` | Counter | pool_id, reason | Failed allocations |
| `cloudpam_discovery_sync_duration_seconds` | Histogram | account_id | Sync duration |
| `cloudpam_discovery_sync_errors_total` | Counter | account_id, error | Sync errors |
| `cloudpam_conflicts_detected_total` | Counter | - | IP conflicts found |

#### Authentication Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cloudpam_auth_attempts_total` | Counter | method, success | Auth attempts |
| `cloudpam_sessions_active` | Gauge | - | Active sessions |
| `cloudpam_api_token_usage_total` | Counter | token_id | API token usage |

#### Audit Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cloudpam_audit_events_total` | Counter | action, resource_type | Audit events |
| `cloudpam_audit_processing_duration_seconds` | Histogram | - | Event processing time |

### 3.3 Prometheus Configuration

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'cloudpam'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        target_label: __address__
        regex: (.+)
        replacement: $1
```

## 4. Distributed Tracing with Jaeger

### 4.1 Trace Propagation

CloudPAM uses W3C Trace Context for distributed tracing:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   API Gateway   │────▶│   CloudPAM      │────▶│   PostgreSQL    │
│                 │     │   Service       │     │                 │
│  traceparent:   │     │  span: "POST    │     │  span: "INSERT  │
│  00-abc123...   │     │   /api/pools"   │     │   pools"        │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                      │                      │
         └──────────────────────┴──────────────────────┘
                               │
                               ▼
                      ┌─────────────────┐
                      │     Jaeger      │
                      │   Collector     │
                      └─────────────────┘
```

### 4.2 Span Attributes

Each span includes:

| Attribute | Example |
|-----------|---------|
| `service.name` | cloudpam |
| `service.version` | 1.0.0 |
| `http.method` | POST |
| `http.url` | /api/v1/pools |
| `http.status_code` | 201 |
| `db.system` | postgresql |
| `db.statement` | INSERT INTO pools... |
| `user.id` | usr_12345 |

### 4.3 Sampling Strategy

| Environment | Strategy | Rate |
|-------------|----------|------|
| Development | Always sample | 100% |
| Staging | Probabilistic | 10% |
| Production | Probabilistic | 1% |

## 5. Audit Logging

### 5.1 Architecture

Audit events are stored in the database and exposed via API:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   HTTP Handler  │────▶│   Audit         │────▶│   audit_events  │
│   Middleware    │     │   Service       │     │   Table         │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                                        │
                                                        ▼
                                               ┌─────────────────┐
                                               │   Audit Log     │
                                               │   API & UI      │
                                               └─────────────────┘
```

### 5.2 Audit Event Schema

```json
{
  "id": "evt_abc123",
  "timestamp": "2025-01-30T14:32:15Z",
  "organization_id": "org_12345",
  "actor": {
    "id": "usr_67890",
    "type": "user",
    "email": "admin@company.com",
    "ip_address": "10.20.30.40"
  },
  "action": "pool.create",
  "resource": {
    "type": "pool",
    "id": "pool_abc123",
    "name": "production-vpc"
  },
  "changes": {
    "before": null,
    "after": {
      "name": "production-vpc",
      "cidr": "10.0.0.0/16"
    }
  },
  "metadata": {
    "source": "web_ui",
    "request_id": "req_xyz789"
  }
}
```

### 5.3 Tracked Actions

| Category | Actions |
|----------|---------|
| **Pools** | create, update, delete, import, allocate |
| **Accounts** | create, update, delete, sync |
| **Users** | create, update, delete, invite, deactivate |
| **Roles** | create, update, delete, assign, revoke |
| **Authentication** | login, logout, token_create, token_revoke |
| **Discovery** | sync_start, sync_complete, sync_failed |
| **Planning** | plan_create, plan_apply, plan_export |
| **System** | drift_detected, conflict_resolved |

### 5.4 Retention & Export

| Feature | Configuration |
|---------|---------------|
| Default retention | 90 days |
| Configurable per org | Yes (OrganizationSettings.AuditRetentionDays) |
| Export formats | CSV, JSON, PDF |
| Real-time export | Syslog, Webhook, S3/GCS |

### 5.5 In-App Audit Log Viewer

The web UI provides a comprehensive audit log viewer with:

- **Timeline view** with color-coded actions
- **Filtering** by action, resource type, actor, date range
- **Search** across event data
- **Detail view** with before/after diff visualization
- **Export** to CSV, JSON, or PDF
- **Statistics** dashboard with trends

See mockup: `cloudpam-audit-log.html`

## 6. Configuration

### 6.1 Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDPAM_LOG_LEVEL` | info | Log level (debug, info, warn, error) |
| `CLOUDPAM_LOG_FORMAT` | json | Log format (json, text) |
| `CLOUDPAM_LOG_SAMPLING_RATE` | 1.0 | Sampling rate for debug/info logs |
| `CLOUDPAM_METRICS_ENABLED` | true | Enable Prometheus metrics |
| `CLOUDPAM_METRICS_PORT` | 8889 | Metrics endpoint port |
| `CLOUDPAM_TRACING_ENABLED` | true | Enable distributed tracing |
| `CLOUDPAM_TRACING_ENDPOINT` | localhost:14250 | Jaeger collector endpoint |
| `CLOUDPAM_TRACING_SAMPLE_RATE` | 0.01 | Trace sampling rate |

### 6.2 Vector Environment Variables

| Variable | Description |
|----------|-------------|
| `SPLUNK_HEC_URL` | Splunk HEC endpoint |
| `SPLUNK_HEC_TOKEN` | Splunk HEC authentication token |
| `AWS_REGION` | AWS region for CloudWatch |
| `GCP_PROJECT_ID` | GCP project for Cloud Logging |
| `SYSLOG_ADDRESS` | Syslog server address |

## 7. Deployment

### 7.1 Local Development

```bash
# Start observability stack
docker-compose -f deploy/docker-compose.observability.yml up -d

# Access dashboards
# Grafana: http://localhost:3000 (admin/admin)
# Prometheus: http://localhost:9090
# Jaeger: http://localhost:16686
```

### 7.2 Kubernetes

```bash
# Deploy observability stack
kubectl apply -f deploy/k8s/observability-stack.yaml

# Deploy Vector DaemonSet
kubectl apply -f deploy/k8s/vector-daemonset.yaml

# Deploy CloudPAM with sidecar
kubectl apply -f deploy/k8s/cloudpam-deployment.yaml
```

### 7.3 Cloud-Specific

| Platform | Logging | Metrics | Tracing |
|----------|---------|---------|---------|
| **GCP** | Cloud Logging (auto) | Cloud Monitoring | Cloud Trace |
| **AWS** | CloudWatch Logs | CloudWatch Metrics | X-Ray |
| **Azure** | Azure Monitor | Azure Monitor | App Insights |

## 8. Grafana Dashboards

Pre-built dashboards available in `deploy/grafana/dashboards/`:

| Dashboard | Panels |
|-----------|--------|
| **CloudPAM Overview** | Request rate, error rate, latency p50/p95/p99 |
| **Pool Management** | Pool utilization, allocation rate, conflicts |
| **Discovery** | Sync frequency, success rate, discovered resources |
| **Authentication** | Login attempts, active sessions, token usage |
| **Database** | Query latency, connection pool, slow queries |

## 9. Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: cloudpam
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: High error rate detected

      - alert: PoolNearCapacity
        expr: cloudpam_pool_utilization_percent > 90
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: Pool {{ $labels.pool_id }} is above 90% utilization

      - alert: DiscoverySyncFailed
        expr: increase(cloudpam_discovery_sync_errors_total[1h]) > 3
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: Discovery sync failing repeatedly
```

## 10. Related Documentation

- [Go Interfaces](internal/observability/interfaces.go) - Logger, Metrics, Tracer interfaces
- [OpenAPI Spec](openapi-observability.yaml) - Audit log API endpoints
- [Vector Config](deploy/vector/vector.toml) - Log shipping configuration
- [K8s Manifests](deploy/k8s/) - Kubernetes deployment files
- [Audit Log UI](cloudpam-audit-log.html) - In-app audit viewer mockup
