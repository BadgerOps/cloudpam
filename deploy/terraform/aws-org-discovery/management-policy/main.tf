####################################################################
# CloudPAM Agent — Management Account IAM Resources
#
# Creates everything the agent needs to run in the management account:
#   1. IAM Role the agent assumes (with instance profile for EC2)
#   2. Policy: organizations:ListAccounts + sts:AssumeRole into members
#   3. Policy: ec2:Describe* for the management account itself
#   4. Policy: sts:GetCallerIdentity for identity verification
####################################################################

variable "role_name" {
  description = "Name of the IAM role in member accounts (must match member-role module)"
  type        = string
  default     = "CloudPAMDiscoveryRole"
}

variable "agent_role_name" {
  description = "Name of the IAM role to create for the CloudPAM agent"
  type        = string
  default     = "CloudPAMAgent"
}

variable "create_instance_profile" {
  description = "Create an EC2 instance profile for the agent role (set false for ECS/Fargate)"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default = {
    ManagedBy = "CloudPAM"
    Purpose   = "Discovery"
  }
}

data "aws_caller_identity" "current" {}
data "aws_organizations_organization" "current" {}
data "aws_partition" "current" {}

# ---------------------------------------------------------------------
# IAM Role for the CloudPAM agent
# ---------------------------------------------------------------------

resource "aws_iam_role" "cloudpam_agent" {
  name = var.agent_role_name

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "EC2AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      },
      {
        Sid    = "ECSAssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = var.tags
}

# Instance profile (for EC2-based agent deployments)
resource "aws_iam_instance_profile" "cloudpam_agent" {
  count = var.create_instance_profile ? 1 : 0
  name  = var.agent_role_name
  role  = aws_iam_role.cloudpam_agent.name

  tags = var.tags
}

# ---------------------------------------------------------------------
# Policy 1: AWS Organizations access + cross-account AssumeRole
# ---------------------------------------------------------------------

resource "aws_iam_policy" "org_discovery" {
  name        = "CloudPAMOrgDiscoveryPolicy"
  description = "List org accounts and assume the discovery role in each member account"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ListOrgAccounts"
        Effect = "Allow"
        Action = [
          "organizations:ListAccounts",
          "organizations:DescribeOrganization",
          "organizations:DescribeAccount"
        ]
        Resource = "*"
      },
      {
        Sid      = "AssumeDiscoveryRole"
        Effect   = "Allow"
        Action   = "sts:AssumeRole"
        Resource = "arn:${data.aws_partition.current.partition}:iam::*:role/${var.role_name}"
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "org_discovery" {
  role       = aws_iam_role.cloudpam_agent.name
  policy_arn = aws_iam_policy.org_discovery.arn
}

# ---------------------------------------------------------------------
# Policy 2: EC2 read-only for management account local discovery
# ---------------------------------------------------------------------

resource "aws_iam_policy" "ec2_discovery" {
  name        = "CloudPAMEC2DiscoveryPolicy"
  description = "Read-only EC2 access for discovering VPCs, subnets, and EIPs"

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

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "ec2_discovery" {
  role       = aws_iam_role.cloudpam_agent.name
  policy_arn = aws_iam_policy.ec2_discovery.arn
}

# ---------------------------------------------------------------------
# Policy 3: STS identity for agent self-identification
# ---------------------------------------------------------------------

resource "aws_iam_policy" "sts_identity" {
  name        = "CloudPAMSTSIdentityPolicy"
  description = "Allow agent to verify its own identity"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "GetCallerIdentity"
        Effect   = "Allow"
        Action   = "sts:GetCallerIdentity"
        Resource = "*"
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "sts_identity" {
  role       = aws_iam_role.cloudpam_agent.name
  policy_arn = aws_iam_policy.sts_identity.arn
}

# ---------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------

output "agent_role_arn" {
  description = "ARN of the IAM role for the CloudPAM agent"
  value       = aws_iam_role.cloudpam_agent.arn
}

output "agent_role_name" {
  description = "Name of the IAM role for the CloudPAM agent"
  value       = aws_iam_role.cloudpam_agent.name
}

output "instance_profile_arn" {
  description = "ARN of the instance profile (if created)"
  value       = var.create_instance_profile ? aws_iam_instance_profile.cloudpam_agent[0].arn : null
}

output "org_discovery_policy_arn" {
  description = "ARN of the Organizations discovery policy"
  value       = aws_iam_policy.org_discovery.arn
}

output "ec2_discovery_policy_arn" {
  description = "ARN of the EC2 discovery policy"
  value       = aws_iam_policy.ec2_discovery.arn
}
