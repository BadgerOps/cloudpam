# CloudPAM Discovery Agent — AWS IAM Configuration
#
# Creates the IAM role and policy needed for the cloudpam-agent to discover
# VPCs, subnets, and Elastic IPs. Supports two modes:
#
#   1. EKS IRSA — set oidc_provider_arn, oidc_provider_url, namespace, service_account_name
#   2. EC2 instance profile — leave OIDC vars empty (defaults)
#
# Usage:
#   terraform init
#   terraform apply -var="oidc_provider_arn=arn:aws:iam::123456789012:oidc-provider/..." \
#                   -var="namespace=cloudpam" -var="service_account_name=cloudpam-agent"

terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 4.0"
    }
  }
}

# ---------- Variables ----------

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "cloudpam-discovery"
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default = {
    ManagedBy = "terraform"
    Component = "cloudpam-discovery"
  }
}

# EKS IRSA variables (leave empty for EC2 instance profile mode)
variable "oidc_provider_arn" {
  description = "ARN of the EKS OIDC provider (e.g. arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE). Leave empty for EC2 mode."
  type        = string
  default     = ""
}

variable "oidc_provider_url" {
  description = "URL of the EKS OIDC provider without https:// (e.g. oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE). Leave empty for EC2 mode."
  type        = string
  default     = ""
}

variable "namespace" {
  description = "Kubernetes namespace where the agent runs (IRSA mode only)"
  type        = string
  default     = "cloudpam"
}

variable "service_account_name" {
  description = "Kubernetes service account name for the agent (IRSA mode only)"
  type        = string
  default     = "cloudpam-agent"
}

# ---------- IAM Policy ----------

data "aws_iam_policy_document" "discovery" {
  statement {
    sid    = "CloudPAMDiscoveryReadOnly"
    effect = "Allow"

    actions = [
      "ec2:DescribeVpcs",
      "ec2:DescribeSubnets",
      "ec2:DescribeAddresses",
    ]

    resources = ["*"]
  }
}

resource "aws_iam_policy" "discovery" {
  name        = "${var.name_prefix}-policy"
  description = "Read-only EC2 networking permissions for CloudPAM discovery agent"
  policy      = data.aws_iam_policy_document.discovery.json
  tags        = var.tags
}

# ---------- Trust Policy ----------

locals {
  use_irsa = var.oidc_provider_arn != ""
}

# IRSA trust policy (EKS pods)
data "aws_iam_policy_document" "irsa_trust" {
  count = local.use_irsa ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [var.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:${var.namespace}:${var.service_account_name}"]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_provider_url}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

# EC2 trust policy (instance profile)
data "aws_iam_policy_document" "ec2_trust" {
  count = local.use_irsa ? 0 : 1

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

# ---------- IAM Role ----------

resource "aws_iam_role" "discovery" {
  name               = "${var.name_prefix}-role"
  assume_role_policy = local.use_irsa ? data.aws_iam_policy_document.irsa_trust[0].json : data.aws_iam_policy_document.ec2_trust[0].json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "discovery" {
  role       = aws_iam_role.discovery.name
  policy_arn = aws_iam_policy.discovery.arn
}

# ---------- EC2 Instance Profile (non-IRSA only) ----------

resource "aws_iam_instance_profile" "discovery" {
  count = local.use_irsa ? 0 : 1
  name  = "${var.name_prefix}-instance-profile"
  role  = aws_iam_role.discovery.name
  tags  = var.tags
}

# ---------- Outputs ----------

output "role_arn" {
  description = "IAM role ARN — use as IRSA annotation or attach to EC2 instances"
  value       = aws_iam_role.discovery.arn
}

output "policy_arn" {
  description = "IAM policy ARN"
  value       = aws_iam_policy.discovery.arn
}

output "instance_profile_name" {
  description = "EC2 instance profile name (only created in EC2 mode)"
  value       = local.use_irsa ? null : aws_iam_instance_profile.discovery[0].name
}

output "helm_set_annotation" {
  description = "Helm --set flag for IRSA role annotation"
  value       = local.use_irsa ? "--set serviceAccount.annotations.\"eks\\.amazonaws\\.com/role-arn\"=${aws_iam_role.discovery.arn}" : null
}
