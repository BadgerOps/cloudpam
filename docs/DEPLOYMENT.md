# CloudPAM Deployment Guide

This document covers deployment options for CloudPAM, from local development to production-grade cloud deployments.

## Architecture Overview

CloudPAM follows a modular architecture that supports multiple deployment patterns:

```
┌─────────────────────────────────────────────────────────────────┐
│                        Load Balancer                            │
│                    (Cloud LB / ALB / nginx)                     │
└─────────────────────────────┬───────────────────────────────────┘
                              │
┌─────────────────────────────▼───────────────────────────────────┐
│                      CloudPAM API Server                        │
│                    (Stateless, Horizontally Scalable)           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   REST API  │  │   Auth/RBAC │  │  Schema     │             │
│  │   Handlers  │  │   Middleware│  │  Planning   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────┬───────────────────────────────────┘
                              │
┌─────────────────────────────▼───────────────────────────────────┐
│                       Data Layer                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  PostgreSQL (Production) / SQLite (Dev/Single-node)     │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                    Discovery Collectors                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │ AWS Collector│  │GCP Collector│  │Azure Collect│             │
│  │   (Optional) │  │  (Optional) │  │  (Future)   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
```

## Deployment Options

| Option | Use Case | Complexity | Scalability |
|--------|----------|------------|-------------|
| Docker Compose | Local dev, small teams | Low | Single node |
| Cloud Run | GCP-native, auto-scaling | Medium | High |
| AWS ECS Fargate | AWS-native, serverless | Medium | High |
| Kubernetes (GKE/EKS) | Multi-cloud, enterprise | High | Very High |

---

## 1. Local Development (Docker Compose)

### Prerequisites
- Docker Engine 24.0+
- Docker Compose v2.20+
- 2GB RAM minimum

### Quick Start

```yaml
# docker-compose.yml
version: '3.9'

services:
  cloudpam:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      - CLOUDPAM_ENV=development
      - CLOUDPAM_DB_TYPE=sqlite
      - CLOUDPAM_DB_PATH=/data/cloudpam.db
      - CLOUDPAM_LOG_LEVEL=debug
      - CLOUDPAM_CORS_ORIGINS=http://localhost:3000
    volumes:
      - cloudpam-data:/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  # Optional: PostgreSQL for production-like testing
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: cloudpam
      POSTGRES_USER: cloudpam
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    profiles:
      - postgres

volumes:
  cloudpam-data:
  postgres-data:
```

### Development Dockerfile

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o cloudpam ./cmd/cloudpam

# Runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates sqlite-libs curl
WORKDIR /app

COPY --from=builder /app/cloudpam .
COPY --from=builder /app/web ./web

EXPOSE 8080
CMD ["./cloudpam"]
```

### Running Locally

```bash
# Start with SQLite (simplest)
docker compose up -d

# Start with PostgreSQL
POSTGRES_PASSWORD=securepassword docker compose --profile postgres up -d

# View logs
docker compose logs -f cloudpam

# Access at http://localhost:8080
```

### Development with Hot Reload

```yaml
# docker-compose.dev.yml
version: '3.9'

services:
  cloudpam-dev:
    build:
      context: .
      dockerfile: Dockerfile.dev
    ports:
      - "8080:8080"
    volumes:
      - .:/app
      - go-mod-cache:/go/pkg/mod
    environment:
      - CLOUDPAM_ENV=development
      - CLOUDPAM_DB_TYPE=sqlite
      - CLOUDPAM_DB_PATH=/data/cloudpam.db
    command: ["air", "-c", ".air.toml"]

volumes:
  go-mod-cache:
```

---

## 2. Google Cloud Run

Cloud Run provides serverless container deployment with automatic scaling and built-in load balancing.

### Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                    Google Cloud Platform                       │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │                    Cloud Run Service                      │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐         │ │
│  │  │ Instance 1 │  │ Instance 2 │  │ Instance N │         │ │
│  │  │ (auto-scaled)│ │           │  │            │         │ │
│  │  └────────────┘  └────────────┘  └────────────┘         │ │
│  └──────────────────────────┬───────────────────────────────┘ │
│                             │                                  │
│  ┌──────────────────────────▼───────────────────────────────┐ │
│  │              Cloud SQL (PostgreSQL)                       │ │
│  │        Private IP via VPC Connector                       │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              Secret Manager                               │ │
│  │    (DB credentials, API keys, OAuth secrets)              │ │
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

### Prerequisites
- Google Cloud project with billing enabled
- gcloud CLI installed and configured
- Artifact Registry repository created

### Terraform Configuration

```hcl
# terraform/gcp/main.tf

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP Region"
  type        = string
  default     = "us-central1"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "production"
}

# Enable required APIs
resource "google_project_service" "services" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "secretmanager.googleapis.com",
    "vpcaccess.googleapis.com",
    "artifactregistry.googleapis.com",
  ])

  project = var.project_id
  service = each.value
}

# VPC for private connectivity
resource "google_compute_network" "vpc" {
  name                    = "cloudpam-vpc"
  project                 = var.project_id
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "subnet" {
  name          = "cloudpam-subnet"
  project       = var.project_id
  region        = var.region
  network       = google_compute_network.vpc.id
  ip_cidr_range = "10.0.0.0/24"
}

# VPC Connector for Cloud Run -> Cloud SQL
resource "google_vpc_access_connector" "connector" {
  name          = "cloudpam-connector"
  project       = var.project_id
  region        = var.region
  ip_cidr_range = "10.8.0.0/28"
  network       = google_compute_network.vpc.name
}

# Cloud SQL PostgreSQL
resource "google_sql_database_instance" "postgres" {
  name             = "cloudpam-db-${var.environment}"
  project          = var.project_id
  region           = var.region
  database_version = "POSTGRES_16"

  settings {
    tier              = "db-f1-micro"  # Adjust for production
    availability_type = "REGIONAL"     # For HA

    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
    }

    backup_configuration {
      enabled                        = true
      start_time                     = "03:00"
      point_in_time_recovery_enabled = true
    }

    database_flags {
      name  = "log_checkpoints"
      value = "on"
    }
  }

  deletion_protection = true
}

resource "google_sql_database" "database" {
  name     = "cloudpam"
  instance = google_sql_database_instance.postgres.name
  project  = var.project_id
}

resource "google_sql_user" "user" {
  name     = "cloudpam"
  instance = google_sql_database_instance.postgres.name
  project  = var.project_id
  password = random_password.db_password.result
}

resource "random_password" "db_password" {
  length  = 32
  special = true
}

# Secret Manager for DB password
resource "google_secret_manager_secret" "db_password" {
  secret_id = "cloudpam-db-password"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "db_password" {
  secret      = google_secret_manager_secret.db_password.id
  secret_data = random_password.db_password.result
}

# Artifact Registry for container images
resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = "cloudpam"
  project       = var.project_id
  format        = "DOCKER"
}

# Service Account for Cloud Run
resource "google_service_account" "cloudpam" {
  account_id   = "cloudpam-service"
  display_name = "CloudPAM Service Account"
  project      = var.project_id
}

resource "google_project_iam_member" "cloudpam_sql" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.cloudpam.email}"
}

resource "google_secret_manager_secret_iam_member" "cloudpam_secret" {
  secret_id = google_secret_manager_secret.db_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudpam.email}"
}

# Cloud Run Service
resource "google_cloud_run_v2_service" "cloudpam" {
  name     = "cloudpam"
  location = var.region
  project  = var.project_id

  template {
    service_account = google_service_account.cloudpam.email

    vpc_access {
      connector = google_vpc_access_connector.connector.id
      egress    = "PRIVATE_RANGES_ONLY"
    }

    containers {
      image = "${var.region}-docker.pkg.dev/${var.project_id}/cloudpam/cloudpam:latest"

      ports {
        container_port = 8080
      }

      env {
        name  = "CLOUDPAM_ENV"
        value = var.environment
      }

      env {
        name  = "CLOUDPAM_DB_TYPE"
        value = "postgres"
      }

      env {
        name  = "CLOUDPAM_DB_HOST"
        value = google_sql_database_instance.postgres.private_ip_address
      }

      env {
        name  = "CLOUDPAM_DB_NAME"
        value = google_sql_database.database.name
      }

      env {
        name  = "CLOUDPAM_DB_USER"
        value = google_sql_user.user.name
      }

      env {
        name = "CLOUDPAM_DB_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.db_password.secret_id
            version = "latest"
          }
        }
      }

      resources {
        limits = {
          cpu    = "1000m"
          memory = "512Mi"
        }
      }

      startup_probe {
        http_get {
          path = "/health"
        }
        initial_delay_seconds = 5
        period_seconds        = 10
        failure_threshold     = 3
      }

      liveness_probe {
        http_get {
          path = "/health"
        }
        period_seconds = 30
      }
    }

    scaling {
      min_instance_count = 1
      max_instance_count = 10
    }
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }
}

# IAM for public access (or use IAP for authenticated access)
resource "google_cloud_run_service_iam_member" "public" {
  location = google_cloud_run_v2_service.cloudpam.location
  project  = google_cloud_run_v2_service.cloudpam.project
  service  = google_cloud_run_v2_service.cloudpam.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

output "service_url" {
  value = google_cloud_run_v2_service.cloudpam.uri
}
```

### Cloud Build Configuration

```yaml
# cloudbuild.yaml
steps:
  # Build the container
  - name: 'gcr.io/cloud-builders/docker'
    args:
      - 'build'
      - '-t'
      - '${_REGION}-docker.pkg.dev/${PROJECT_ID}/cloudpam/cloudpam:${SHORT_SHA}'
      - '-t'
      - '${_REGION}-docker.pkg.dev/${PROJECT_ID}/cloudpam/cloudpam:latest'
      - '.'

  # Push to Artifact Registry
  - name: 'gcr.io/cloud-builders/docker'
    args:
      - 'push'
      - '--all-tags'
      - '${_REGION}-docker.pkg.dev/${PROJECT_ID}/cloudpam/cloudpam'

  # Deploy to Cloud Run
  - name: 'gcr.io/google.com/cloudsdktool/cloud-sdk'
    entrypoint: gcloud
    args:
      - 'run'
      - 'deploy'
      - 'cloudpam'
      - '--image'
      - '${_REGION}-docker.pkg.dev/${PROJECT_ID}/cloudpam/cloudpam:${SHORT_SHA}'
      - '--region'
      - '${_REGION}'
      - '--platform'
      - 'managed'

substitutions:
  _REGION: us-central1

options:
  logging: CLOUD_LOGGING_ONLY
```

---

## 3. AWS Deployment (ECS Fargate)

ECS Fargate provides serverless container orchestration with deep AWS integration.

### Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                        AWS Account                             │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │                Application Load Balancer                  │ │
│  │              (HTTPS termination, routing)                 │ │
│  └──────────────────────────┬───────────────────────────────┘ │
│                             │                                  │
│  ┌──────────────────────────▼───────────────────────────────┐ │
│  │                    ECS Fargate Cluster                    │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐         │ │
│  │  │   Task 1   │  │   Task 2   │  │   Task N   │         │ │
│  │  │ (auto-scaled)│ │           │  │            │         │ │
│  │  └────────────┘  └────────────┘  └────────────┘         │ │
│  └──────────────────────────┬───────────────────────────────┘ │
│                             │                                  │
│  ┌──────────────────────────▼───────────────────────────────┐ │
│  │                    RDS PostgreSQL                         │ │
│  │              (Multi-AZ for production)                    │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │                  Secrets Manager                          │ │
│  │        (DB credentials, OAuth client secrets)             │ │
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

### Terraform Configuration

```hcl
# terraform/aws/main.tf

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "region" {
  default = "us-east-1"
}

variable "environment" {
  default = "production"
}

provider "aws" {
  region = var.region
}

# VPC
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"

  name = "cloudpam-vpc"
  cidr = "10.0.0.0/16"

  azs             = ["${var.region}a", "${var.region}b", "${var.region}c"]
  private_subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24", "10.0.103.0/24"]

  enable_nat_gateway = true
  single_nat_gateway = var.environment != "production"

  enable_dns_hostnames = true
  enable_dns_support   = true
}

# Security Groups
resource "aws_security_group" "alb" {
  name        = "cloudpam-alb-sg"
  description = "ALB security group"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "ecs" {
  name        = "cloudpam-ecs-sg"
  description = "ECS tasks security group"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "rds" {
  name        = "cloudpam-rds-sg"
  description = "RDS security group"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs.id]
  }
}

# RDS PostgreSQL
resource "aws_db_subnet_group" "main" {
  name       = "cloudpam-db-subnet"
  subnet_ids = module.vpc.private_subnets
}

resource "random_password" "db_password" {
  length  = 32
  special = true
}

resource "aws_secretsmanager_secret" "db_password" {
  name = "cloudpam/db-password"
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id = aws_secretsmanager_secret.db_password.id
  secret_string = jsonencode({
    username = "cloudpam"
    password = random_password.db_password.result
  })
}

resource "aws_db_instance" "postgres" {
  identifier     = "cloudpam-${var.environment}"
  engine         = "postgres"
  engine_version = "16.1"
  instance_class = "db.t3.micro"  # Adjust for production

  allocated_storage     = 20
  max_allocated_storage = 100
  storage_encrypted     = true

  db_name  = "cloudpam"
  username = "cloudpam"
  password = random_password.db_password.result

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  multi_az               = var.environment == "production"
  backup_retention_period = 7
  skip_final_snapshot    = var.environment != "production"

  performance_insights_enabled = true
}

# ECR Repository
resource "aws_ecr_repository" "cloudpam" {
  name                 = "cloudpam"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }
}

# ECS Cluster
resource "aws_ecs_cluster" "main" {
  name = "cloudpam-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

# ECS Task Execution Role
resource "aws_iam_role" "ecs_execution" {
  name = "cloudpam-ecs-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_execution" {
  role       = aws_iam_role.ecs_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "ecs_secrets" {
  name = "cloudpam-secrets-access"
  role = aws_iam_role.ecs_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "secretsmanager:GetSecretValue"
      ]
      Resource = [aws_secretsmanager_secret.db_password.arn]
    }]
  })
}

# ECS Task Role (for application permissions)
resource "aws_iam_role" "ecs_task" {
  name = "cloudpam-ecs-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })
}

# Add permissions for cloud discovery (AWS)
resource "aws_iam_role_policy" "cloud_discovery" {
  name = "cloudpam-cloud-discovery"
  role = aws_iam_role.ecs_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DescribeAddresses"
      ]
      Resource = "*"
    }]
  })
}

# CloudWatch Log Group
resource "aws_cloudwatch_log_group" "cloudpam" {
  name              = "/ecs/cloudpam"
  retention_in_days = 30
}

# ECS Task Definition
resource "aws_ecs_task_definition" "cloudpam" {
  family                   = "cloudpam"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_execution.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([{
    name  = "cloudpam"
    image = "${aws_ecr_repository.cloudpam.repository_url}:latest"

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = [
      { name = "CLOUDPAM_ENV", value = var.environment },
      { name = "CLOUDPAM_DB_TYPE", value = "postgres" },
      { name = "CLOUDPAM_DB_HOST", value = aws_db_instance.postgres.address },
      { name = "CLOUDPAM_DB_NAME", value = "cloudpam" },
    ]

    secrets = [
      {
        name      = "CLOUDPAM_DB_USER"
        valueFrom = "${aws_secretsmanager_secret.db_password.arn}:username::"
      },
      {
        name      = "CLOUDPAM_DB_PASSWORD"
        valueFrom = "${aws_secretsmanager_secret.db_password.arn}:password::"
      }
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.cloudpam.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "ecs"
      }
    }

    healthCheck = {
      command     = ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 60
    }
  }])
}

# Application Load Balancer
resource "aws_lb" "main" {
  name               = "cloudpam-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = module.vpc.public_subnets
}

resource "aws_lb_target_group" "cloudpam" {
  name        = "cloudpam-tg"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = module.vpc.vpc_id
  target_type = "ip"

  health_check {
    enabled             = true
    healthy_threshold   = 2
    unhealthy_threshold = 3
    timeout             = 5
    interval            = 30
    path                = "/health"
    matcher             = "200"
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

# Note: You'll need an ACM certificate for HTTPS
# resource "aws_lb_listener" "https" { ... }

# ECS Service
resource "aws_ecs_service" "cloudpam" {
  name            = "cloudpam"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.cloudpam.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = module.vpc.private_subnets
    security_groups  = [aws_security_group.ecs.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.cloudpam.arn
    container_name   = "cloudpam"
    container_port   = 8080
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
}

# Auto Scaling
resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = 10
  min_capacity       = 2
  resource_id        = "service/${aws_ecs_cluster.main.name}/${aws_ecs_service.cloudpam.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

resource "aws_appautoscaling_policy" "cpu" {
  name               = "cloudpam-cpu-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    target_value       = 70.0
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

output "alb_dns_name" {
  value = aws_lb.main.dns_name
}

output "ecr_repository_url" {
  value = aws_ecr_repository.cloudpam.repository_url
}
```

---

## 4. Kubernetes (GKE/EKS)

For enterprise deployments requiring maximum control and multi-cloud portability.

### Helm Chart Structure

```
helm/cloudpam/
├── Chart.yaml
├── values.yaml
├── values-gke.yaml
├── values-eks.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── hpa.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   └── serviceaccount.yaml
```

### Chart.yaml

```yaml
# helm/cloudpam/Chart.yaml
apiVersion: v2
name: cloudpam
description: Cloud-native IP Address Management
version: 0.1.0
appVersion: "0.1.0"
```

### values.yaml

```yaml
# helm/cloudpam/values.yaml
replicaCount: 2

image:
  repository: cloudpam
  tag: latest
  pullPolicy: IfNotPresent

serviceAccount:
  create: true
  annotations: {}

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: cloudpam.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: cloudpam-tls
      hosts:
        - cloudpam.example.com

resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

config:
  environment: production
  logLevel: info

database:
  type: postgres
  host: ""
  name: cloudpam
  existingSecret: cloudpam-db-credentials

# Cloud provider specific
cloudProvider: ""  # gke or eks
```

### Deployment Template

```yaml
# helm/cloudpam/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "cloudpam.fullname" . }}
  labels:
    {{- include "cloudpam.labels" . | nindent 4 }}
spec:
  {{- if not .Values.autoscaling.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "cloudpam.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "cloudpam.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "cloudpam.serviceAccountName" . }}
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          env:
            - name: CLOUDPAM_ENV
              value: {{ .Values.config.environment }}
            - name: CLOUDPAM_LOG_LEVEL
              value: {{ .Values.config.logLevel }}
            - name: CLOUDPAM_DB_TYPE
              value: {{ .Values.database.type }}
            - name: CLOUDPAM_DB_HOST
              value: {{ .Values.database.host }}
            - name: CLOUDPAM_DB_NAME
              value: {{ .Values.database.name }}
            - name: CLOUDPAM_DB_USER
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.database.existingSecret }}
                  key: username
            - name: CLOUDPAM_DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.database.existingSecret }}
                  key: password
          livenessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 15
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
```

### GKE-specific Values

```yaml
# helm/cloudpam/values-gke.yaml
cloudProvider: gke

serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: cloudpam@PROJECT_ID.iam.gserviceaccount.com

ingress:
  className: gce
  annotations:
    kubernetes.io/ingress.global-static-ip-name: cloudpam-ip
    networking.gke.io/managed-certificates: cloudpam-cert
```

### EKS-specific Values

```yaml
# helm/cloudpam/values-eks.yaml
cloudProvider: eks

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/cloudpam-role

ingress:
  className: alb
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/certificate-arn: arn:aws:acm:REGION:ACCOUNT:certificate/CERT_ID
```

---

## Environment Variables Reference

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CLOUDPAM_ENV` | Environment (development/staging/production) | development | No |
| `CLOUDPAM_DB_TYPE` | Database type (sqlite/postgres) | sqlite | No |
| `CLOUDPAM_DB_PATH` | SQLite file path | ./cloudpam.db | SQLite only |
| `CLOUDPAM_DB_HOST` | PostgreSQL host | - | Postgres only |
| `CLOUDPAM_DB_PORT` | PostgreSQL port | 5432 | No |
| `CLOUDPAM_DB_NAME` | Database name | cloudpam | Postgres only |
| `CLOUDPAM_DB_USER` | Database user | - | Postgres only |
| `CLOUDPAM_DB_PASSWORD` | Database password | - | Postgres only |
| `CLOUDPAM_DB_SSL_MODE` | SSL mode (disable/require/verify-full) | require | No |
| `CLOUDPAM_LOG_LEVEL` | Log level (debug/info/warn/error) | info | No |
| `CLOUDPAM_LOG_FORMAT` | Log format (json/text) | json | No |
| `CLOUDPAM_PORT` | HTTP server port | 8080 | No |
| `CLOUDPAM_CORS_ORIGINS` | Allowed CORS origins | - | No |
| `CLOUDPAM_OAUTH_ISSUER` | OIDC issuer URL | - | OAuth only |
| `CLOUDPAM_OAUTH_CLIENT_ID` | OAuth client ID | - | OAuth only |
| `CLOUDPAM_OAUTH_CLIENT_SECRET` | OAuth client secret | - | OAuth only |
| `CLOUDPAM_SENTRY_DSN` | Sentry DSN for error tracking | - | No |

---

## Security Best Practices

### Network Security
- Deploy database in private subnets only
- Use VPC peering or private endpoints for cloud APIs
- Enable TLS everywhere (ALB/Ingress terminates, internal can be HTTP in VPC)
- Restrict security groups to minimum required ports

### Secrets Management
- Never commit secrets to version control
- Use cloud-native secrets managers (Secret Manager, Secrets Manager)
- Rotate database passwords regularly
- Use short-lived credentials where possible (Workload Identity, IRSA)

### IAM & Access
- Follow principle of least privilege
- Use service accounts with specific permissions
- Enable audit logging for all admin actions
- Implement RBAC at application level

### Container Security
- Run containers as non-root
- Use read-only root filesystem
- Scan images for vulnerabilities
- Pin image versions (avoid :latest in production)

---

## Monitoring & Observability

### Recommended Stack
- **Metrics**: Prometheus + Grafana (or Cloud Monitoring)
- **Logging**: Structured JSON logs → Cloud Logging / CloudWatch
- **Tracing**: OpenTelemetry → Cloud Trace / X-Ray
- **Alerting**: PagerDuty / Opsgenie integration

### Key Metrics to Monitor
- Request latency (p50, p95, p99)
- Error rate (4xx, 5xx)
- Database connection pool usage
- Discovery sync success/failure rate
- Memory and CPU utilization

### Health Endpoints
- `GET /health` - Basic liveness check
- `GET /ready` - Readiness including DB connectivity
- `GET /metrics` - Prometheus metrics (future)

---

## Disaster Recovery

### Backup Strategy
- PostgreSQL: Daily automated backups, point-in-time recovery enabled
- Retain backups for 7 days (dev) / 30 days (prod)
- Test restore procedures quarterly

### Multi-Region (Future)
- Active-passive with read replicas
- DNS failover with health checks
- Database replication lag monitoring

---

## Cost Optimization

| Component | Dev/Small | Production |
|-----------|-----------|------------|
| Cloud Run | Min instances: 0-1 | Min instances: 2-3 |
| ECS Fargate | 0.25 vCPU, 0.5GB | 0.5-1 vCPU, 1-2GB |
| Database | db.t3.micro / db-f1-micro | db.t3.medium / db-custom |
| NAT Gateway | Single | Per-AZ (optional) |

### Cost-Saving Tips
- Use committed use discounts for stable workloads
- Schedule dev environments to shut down outside business hours
- Enable auto-scaling with appropriate min/max bounds
- Use spot/preemptible instances for non-critical workloads
