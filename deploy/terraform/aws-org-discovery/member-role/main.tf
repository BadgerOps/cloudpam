####################################################################
# CloudPAM Discovery Role — Member Account
#
# Deploy to each member account (via StackSet or per-account apply).
# Creates:
#   1. IAM Role trusted by the management account's agent role
#   2. Inline policy with read-only EC2 permissions for discovery
####################################################################

variable "management_account_id" {
  description = "AWS account ID of the management account where the agent runs"
  type        = string
}

variable "agent_role_name" {
  description = "Name of the IAM role the agent uses in the management account"
  type        = string
  default     = "CloudPAMAgent"
}

variable "role_name" {
  description = "Name of the IAM role to create in this member account"
  type        = string
  default     = "CloudPAMDiscoveryRole"
}

variable "external_id" {
  description = "External ID for the trust policy (optional but recommended)"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default = {
    ManagedBy = "CloudPAM"
    Purpose   = "Discovery"
  }
}

data "aws_partition" "current" {}

# ---------------------------------------------------------------------
# IAM Role — trusted by management account agent
# ---------------------------------------------------------------------

resource "aws_iam_role" "cloudpam_discovery" {
  name = var.role_name

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowManagementAgent"
        Effect = "Allow"
        Principal = {
          AWS = "arn:${data.aws_partition.current.partition}:iam::${var.management_account_id}:role/${var.agent_role_name}"
        }
        Action = "sts:AssumeRole"
        Condition = var.external_id != "" ? {
          StringEquals = {
            "sts:ExternalId" = var.external_id
          }
        } : {}
      }
    ]
  })

  max_session_duration = 3600 # 1 hour — enough for a discovery pass

  tags = var.tags
}

# ---------------------------------------------------------------------
# Inline policy — read-only EC2 for VPC/subnet/EIP discovery
# ---------------------------------------------------------------------

resource "aws_iam_role_policy" "ec2_read_only" {
  name = "CloudPAMEC2ReadOnly"
  role = aws_iam_role.cloudpam_discovery.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "EC2ReadOnly"
        Effect = "Allow"
        Action = [
          "ec2:DescribeVpcs",
          "ec2:DescribeSubnets",
          "ec2:DescribeAddresses",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DescribeRegions"
        ]
        Resource = "*"
      }
    ]
  })
}

# ---------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------

output "role_arn" {
  description = "ARN of the discovery role in this member account"
  value       = aws_iam_role.cloudpam_discovery.arn
}

output "role_name" {
  description = "Name of the discovery role"
  value       = aws_iam_role.cloudpam_discovery.name
}
