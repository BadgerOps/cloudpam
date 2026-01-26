# feat: Add Cloud Run deployment with Terraform and GitHub Actions

## Summary

Implement automated Cloud Run deployment using Terraform for infrastructure-as-code and GitHub Actions for CI/CD. This enables cost-effective, scalable production hosting on GCP with ~$0-10/month operational costs.

## Motivation

- **Cost-effective**: Cloud Run free tier covers most light workloads (2M requests/month free)
- **Scalable**: Auto-scales from 0 to handle traffic spikes
- **Managed**: No server maintenance, automatic TLS, load balancing
- **GitOps**: Infrastructure changes tracked in version control

## Implementation Plan

### Phase 1: Containerization

#### 1.1 Create Dockerfile

Create a multi-stage Dockerfile optimized for Cloud Run:

```dockerfile
# Dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -tags sqlite -ldflags="-s -w" -o cloudpam ./cmd/cloudpam

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/cloudpam /cloudpam

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/cloudpam"]
```

**Key decisions:**
- Use `distroless` for minimal attack surface (~2MB base)
- Build with `-tags sqlite` for persistence support
- Run as non-root user
- CGO disabled (modernc.org/sqlite is pure Go)

#### 1.2 Add .dockerignore

```
.git
.github
*.md
docs/
photos/
*.db
*.sqlite*
coverage.*
.env*
terraform/
```

### Phase 2: Terraform Infrastructure

#### 2.1 Directory Structure

```
terraform/
├── environments/
│   ├── staging/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   ├── terraform.tfvars      # Git-ignored
│   │   └── backend.tf
│   └── production/
│       ├── main.tf
│       ├── variables.tf
│       ├── terraform.tfvars      # Git-ignored
│       └── backend.tf
├── modules/
│   ├── cloud-run/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   ├── artifact-registry/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   └── secrets/
│       ├── main.tf
│       ├── variables.tf
│       └── outputs.tf
└── README.md
```

#### 2.2 Cloud Run Module (`modules/cloud-run/main.tf`)

```hcl
resource "google_cloud_run_v2_service" "cloudpam" {
  name     = var.service_name
  location = var.region

  template {
    containers {
      image = var.image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = var.cpu
          memory = var.memory
        }
        cpu_idle = true  # Scale to zero
      }

      # Non-sensitive environment variables
      env {
        name  = "LOG_LEVEL"
        value = var.log_level
      }

      env {
        name  = "RATE_LIMIT_RPS"
        value = tostring(var.rate_limit_rps)
      }

      env {
        name  = "RATE_LIMIT_BURST"
        value = tostring(var.rate_limit_burst)
      }

      env {
        name  = "APP_VERSION"
        value = var.app_version
      }

      env {
        name  = "SENTRY_ENVIRONMENT"
        value = var.environment
      }

      # Secrets from Secret Manager
      dynamic "env" {
        for_each = var.sentry_dsn_secret != null ? [1] : []
        content {
          name = "SENTRY_DSN"
          value_source {
            secret_key_ref {
              secret  = var.sentry_dsn_secret
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.sentry_frontend_dsn_secret != null ? [1] : []
        content {
          name = "SENTRY_FRONTEND_DSN"
          value_source {
            secret_key_ref {
              secret  = var.sentry_frontend_dsn_secret
              version = "latest"
            }
          }
        }
      }

      startup_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
        initial_delay_seconds = 5
        timeout_seconds       = 3
        period_seconds        = 5
        failure_threshold     = 3
      }

      liveness_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
        timeout_seconds   = 3
        period_seconds    = 30
        failure_threshold = 3
      }
    }

    scaling {
      min_instance_count = var.min_instances
      max_instance_count = var.max_instances
    }

    service_account = google_service_account.cloudpam.email

    execution_environment = "EXECUTION_ENVIRONMENT_GEN2"
  }

  traffic {
    percent = 100
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
  }
}

# Service Account
resource "google_service_account" "cloudpam" {
  account_id   = "${var.service_name}-sa"
  display_name = "CloudPAM Service Account"
}

# IAM: Allow unauthenticated access (optional, controlled by variable)
resource "google_cloud_run_v2_service_iam_member" "public" {
  count    = var.allow_unauthenticated ? 1 : 0
  location = google_cloud_run_v2_service.cloudpam.location
  name     = google_cloud_run_v2_service.cloudpam.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# IAM: Service account can access secrets
resource "google_secret_manager_secret_iam_member" "sentry_dsn" {
  count     = var.sentry_dsn_secret != null ? 1 : 0
  secret_id = var.sentry_dsn_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudpam.email}"
}

resource "google_secret_manager_secret_iam_member" "sentry_frontend_dsn" {
  count     = var.sentry_frontend_dsn_secret != null ? 1 : 0
  secret_id = var.sentry_frontend_dsn_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudpam.email}"
}
```

#### 2.3 Artifact Registry Module

```hcl
resource "google_artifact_registry_repository" "cloudpam" {
  location      = var.region
  repository_id = var.repository_name
  format        = "DOCKER"
  description   = "CloudPAM container images"

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }
}
```

#### 2.4 Remote State Backend

```hcl
# backend.tf
terraform {
  backend "gcs" {
    bucket = "cloudpam-terraform-state"
    prefix = "staging"  # or "production"
  }
}
```

### Phase 3: GitHub Actions Deployment

#### 3.1 Workflow: `.github/workflows/deploy.yml`

```yaml
name: Deploy to Cloud Run

on:
  release:
    types: [published]
  workflow_dispatch:
    inputs:
      environment:
        description: 'Deployment environment'
        required: true
        type: choice
        options:
          - staging
          - production
        default: staging

env:
  PROJECT_ID: ${{ secrets.GCP_PROJECT_ID }}
  REGION: us-central1
  SERVICE_NAME: cloudpam
  REGISTRY: us-central1-docker.pkg.dev

jobs:
  build:
    name: Build and Push Image
    runs-on: ubuntu-latest
    outputs:
      image: ${{ steps.build.outputs.image }}
      digest: ${{ steps.build.outputs.digest }}

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Authenticate to Google Cloud
        uses: google-github-actions/auth@v2
        with:
          credentials_json: ${{ secrets.GCP_SA_KEY }}

      - name: Set up Cloud SDK
        uses: google-github-actions/setup-gcloud@v2

      - name: Configure Docker
        run: gcloud auth configure-docker ${{ env.REGISTRY }}

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Extract version
        id: version
        run: |
          if [ "${{ github.event_name }}" = "release" ]; then
            echo "version=${{ github.event.release.tag_name }}" >> $GITHUB_OUTPUT
          else
            echo "version=${{ github.sha }}" >> $GITHUB_OUTPUT
          fi

      - name: Build and Push
        id: build
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: |
            ${{ env.REGISTRY }}/${{ env.PROJECT_ID }}/cloudpam/cloudpam:${{ steps.version.outputs.version }}
            ${{ env.REGISTRY }}/${{ env.PROJECT_ID }}/cloudpam/cloudpam:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Output image reference
        run: |
          echo "image=${{ env.REGISTRY }}/${{ env.PROJECT_ID }}/cloudpam/cloudpam@${{ steps.build.outputs.digest }}" >> $GITHUB_OUTPUT

  deploy:
    name: Deploy with Terraform
    needs: build
    runs-on: ubuntu-latest
    environment: ${{ github.event.inputs.environment || 'staging' }}

    defaults:
      run:
        working-directory: terraform/environments/${{ github.event.inputs.environment || 'staging' }}

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Authenticate to Google Cloud
        uses: google-github-actions/auth@v2
        with:
          credentials_json: ${{ secrets.GCP_SA_KEY }}

      - name: Setup Terraform
        uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: 1.7.0

      - name: Terraform Init
        run: terraform init

      - name: Terraform Plan
        id: plan
        run: |
          terraform plan -no-color \
            -var="image=${{ needs.build.outputs.image }}" \
            -var="app_version=${{ github.event.release.tag_name || github.sha }}" \
            -out=tfplan
        continue-on-error: true

      - name: Terraform Apply
        if: steps.plan.outcome == 'success'
        run: terraform apply -auto-approve tfplan

      - name: Get Service URL
        id: url
        run: |
          url=$(terraform output -raw service_url)
          echo "url=${url}" >> $GITHUB_OUTPUT

      - name: Smoke Test
        run: |
          echo "Testing ${{ steps.url.outputs.url }}/healthz"
          for i in {1..10}; do
            if curl -sf "${{ steps.url.outputs.url }}/healthz" > /dev/null; then
              echo "Health check passed!"
              exit 0
            fi
            echo "Attempt $i failed, waiting..."
            sleep 5
          done
          echo "Health check failed after 10 attempts"
          exit 1

      - name: Summary
        run: |
          echo "## Deployment Summary" >> $GITHUB_STEP_SUMMARY
          echo "- **Environment:** ${{ github.event.inputs.environment || 'staging' }}" >> $GITHUB_STEP_SUMMARY
          echo "- **Version:** ${{ github.event.release.tag_name || github.sha }}" >> $GITHUB_STEP_SUMMARY
          echo "- **URL:** ${{ steps.url.outputs.url }}" >> $GITHUB_STEP_SUMMARY
```

#### 3.2 Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `GCP_PROJECT_ID` | GCP project ID |
| `GCP_SA_KEY` | Service account JSON key with required permissions |

#### 3.3 Required GCP Permissions for Service Account

```
roles/run.admin                    # Deploy Cloud Run services
roles/iam.serviceAccountUser       # Act as service accounts
roles/artifactregistry.writer      # Push container images
roles/secretmanager.admin          # Manage secrets (initial setup)
roles/storage.admin                # Terraform state bucket
```

### Phase 4: Environment Configuration

#### 4.1 Staging Environment

```hcl
# terraform/environments/staging/variables.tf
variable "project_id" {
  default = "your-project-id"
}

variable "region" {
  default = "us-central1"
}

variable "environment" {
  default = "staging"
}

# terraform/environments/staging/main.tf
module "artifact_registry" {
  source          = "../../modules/artifact-registry"
  region          = var.region
  repository_name = "cloudpam"
}

module "cloud_run" {
  source       = "../../modules/cloud-run"
  service_name = "cloudpam-staging"
  region       = var.region
  image        = var.image
  environment  = "staging"

  # Staging: minimal resources, public access
  cpu                    = "1"
  memory                 = "512Mi"
  min_instances          = 0
  max_instances          = 2
  allow_unauthenticated  = true

  # Optional: Sentry
  sentry_dsn_secret          = null  # Or reference secret
  sentry_frontend_dsn_secret = null

  app_version     = var.app_version
  log_level       = "debug"
  rate_limit_rps  = 10
  rate_limit_burst = 20
}
```

#### 4.2 Production Environment

```hcl
# terraform/environments/production/main.tf
module "cloud_run" {
  source       = "../../modules/cloud-run"
  service_name = "cloudpam"
  region       = var.region
  image        = var.image
  environment  = "production"

  # Production: more resources, consider authentication
  cpu                    = "2"
  memory                 = "1Gi"
  min_instances          = 1    # Always warm
  max_instances          = 10
  allow_unauthenticated  = true # Or false with IAP

  # Sentry for production monitoring
  sentry_dsn_secret          = google_secret_manager_secret.sentry_dsn.id
  sentry_frontend_dsn_secret = google_secret_manager_secret.sentry_frontend_dsn.id

  app_version     = var.app_version
  log_level       = "info"
  rate_limit_rps  = 100
  rate_limit_burst = 200
}
```

### Phase 5: Initial Setup & Maintenance

#### 5.1 One-Time Setup Steps

1. **Create GCP Project**
   ```bash
   gcloud projects create cloudpam-prod --name="CloudPAM Production"
   gcloud config set project cloudpam-prod
   ```

2. **Enable Required APIs**
   ```bash
   gcloud services enable \
     run.googleapis.com \
     artifactregistry.googleapis.com \
     secretmanager.googleapis.com \
     cloudbuild.googleapis.com \
     iam.googleapis.com
   ```

3. **Create Terraform State Bucket**
   ```bash
   gsutil mb -l us-central1 gs://cloudpam-terraform-state
   gsutil versioning set on gs://cloudpam-terraform-state
   ```

4. **Create Service Account for GitHub Actions**
   ```bash
   gcloud iam service-accounts create github-actions \
     --display-name="GitHub Actions"

   # Grant permissions
   for role in run.admin iam.serviceAccountUser \
     artifactregistry.writer secretmanager.admin storage.admin; do
     gcloud projects add-iam-policy-binding cloudpam-prod \
       --member="serviceAccount:github-actions@cloudpam-prod.iam.gserviceaccount.com" \
       --role="roles/${role}"
   done

   # Create and download key
   gcloud iam service-accounts keys create github-actions-key.json \
     --iam-account=github-actions@cloudpam-prod.iam.gserviceaccount.com
   ```

5. **Add GitHub Secrets**
   ```bash
   gh secret set GCP_PROJECT_ID --body "cloudpam-prod"
   gh secret set GCP_SA_KEY < github-actions-key.json
   rm github-actions-key.json  # Don't keep locally
   ```

6. **Create Sentry Secrets (Optional)**
   ```bash
   echo -n "https://xxx@sentry.io/xxx" | \
     gcloud secrets create sentry-dsn --data-file=-

   echo -n "https://xxx@sentry.io/xxx" | \
     gcloud secrets create sentry-frontend-dsn --data-file=-
   ```

7. **Initialize Terraform**
   ```bash
   cd terraform/environments/staging
   terraform init
   terraform plan
   terraform apply
   ```

#### 5.2 Ongoing Maintenance

| Task | Frequency | Command |
|------|-----------|---------|
| Deploy new version | On release | Automatic via GitHub Actions |
| View logs | As needed | `gcloud run logs read cloudpam` |
| Check metrics | Weekly | Cloud Console → Cloud Run → Metrics |
| Update Terraform | As needed | PR with changes → merge → auto-apply |
| Rotate SA keys | Quarterly | Recreate key, update GitHub secret |
| Review costs | Monthly | Cloud Console → Billing |

#### 5.3 Rollback Procedure

```bash
# Option 1: Redeploy previous image via workflow dispatch
# Select previous version tag in GitHub Actions

# Option 2: Manual rollback
gcloud run services update-traffic cloudpam \
  --to-revisions=cloudpam-00001-abc=100 \
  --region=us-central1

# Option 3: Terraform rollback
cd terraform/environments/production
terraform apply -var="image=PREVIOUS_IMAGE_DIGEST"
```

## Files to Create

- [ ] `Dockerfile`
- [ ] `.dockerignore`
- [ ] `terraform/modules/cloud-run/main.tf`
- [ ] `terraform/modules/cloud-run/variables.tf`
- [ ] `terraform/modules/cloud-run/outputs.tf`
- [ ] `terraform/modules/artifact-registry/main.tf`
- [ ] `terraform/modules/artifact-registry/variables.tf`
- [ ] `terraform/modules/artifact-registry/outputs.tf`
- [ ] `terraform/modules/secrets/main.tf`
- [ ] `terraform/modules/secrets/variables.tf`
- [ ] `terraform/modules/secrets/outputs.tf`
- [ ] `terraform/environments/staging/main.tf`
- [ ] `terraform/environments/staging/variables.tf`
- [ ] `terraform/environments/staging/outputs.tf`
- [ ] `terraform/environments/staging/backend.tf`
- [ ] `terraform/environments/production/main.tf`
- [ ] `terraform/environments/production/variables.tf`
- [ ] `terraform/environments/production/outputs.tf`
- [ ] `terraform/environments/production/backend.tf`
- [ ] `terraform/README.md`
- [ ] `.github/workflows/deploy.yml`
- [ ] `docs/DEPLOYMENT.md`
- [ ] Update `.gitignore` for Terraform files

## Estimated Costs

| Resource | Staging | Production |
|----------|---------|------------|
| Cloud Run | $0-5/mo (scale to zero) | $5-30/mo (min 1 instance) |
| Artifact Registry | $0.10/GB/mo | $0.10/GB/mo |
| Secret Manager | $0.06/secret/mo | $0.06/secret/mo |
| Terraform State (GCS) | ~$0.02/mo | ~$0.02/mo |
| **Total** | **~$0-6/mo** | **~$6-35/mo** |

## Acceptance Criteria

- [ ] Docker image builds successfully with `-tags sqlite`
- [ ] Terraform plans and applies without errors
- [ ] GitHub Actions workflow deploys on release
- [ ] Health check passes after deployment
- [ ] Logs visible in Cloud Logging
- [ ] Sentry receives test events (if configured)
- [ ] Rollback procedure documented and tested
- [ ] Cost stays within estimated range

## Labels

`enhancement`, `infrastructure`, `documentation`
