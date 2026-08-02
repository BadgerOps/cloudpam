import { hclIdent, hclQuote } from '../../utils/escape'
import type { SchemaNode } from './cidr'

/**
 * Renders a generated schema as Terraform for the CloudPAM provider
 * (`terraform-provider-cloudpam`), which exposes a `cloudpam_pool` resource
 * with name/cidr/type/parent_id/tags attributes.
 *
 * Parent/child links are emitted as `cloudpam_pool.<label>.id` references, so
 * Terraform creates pools in dependency order without the plan hard-coding IDs
 * that only exist in one CloudPAM instance.
 */

/**
 * Assigns a unique, valid HCL resource label to every node. Pool names are
 * user-supplied and may collide or contain characters HCL rejects, so the
 * sanitised label is de-duplicated with a numeric suffix.
 */
function assignLabels(root: SchemaNode): Map<string, string> {
  const labels = new Map<string, string>()
  const used = new Set<string>()

  const visit = (node: SchemaNode) => {
    const base = hclIdent(node.name.toLowerCase().replace(/\s+/g, '_'), 'pool')
    let label = base
    let n = 2
    while (used.has(label)) {
      label = `${base}_${n}`
      n++
    }
    used.add(label)
    labels.set(node.id, label)
    for (const child of node.children ?? []) visit(child)
  }

  visit(root)
  return labels
}

function renderResource(node: SchemaNode, label: string, parentLabel: string | null): string {
  const lines = [
    `resource "cloudpam_pool" ${hclQuote(label)} {`,
    `  name = ${hclQuote(node.name)}`,
    `  cidr = ${hclQuote(node.cidr)}`,
    `  type = ${hclQuote(node.type)}`,
  ]
  if (parentLabel) {
    // A resource reference, not a quoted string: this is the dependency edge.
    lines.push(`  parent_id = cloudpam_pool.${parentLabel}.id`)
  }
  lines.push('')
  lines.push('  tags = {')
  lines.push(`    managed_by = ${hclQuote('terraform')}`)
  lines.push(`    source     = ${hclQuote('cloudpam-schema-planner')}`)
  lines.push('  }')
  lines.push('}')
  return lines.join('\n')
}

export function schemaToTerraform(root: SchemaNode, generatedAt: string): string {
  const labels = assignLabels(root)
  const blocks: string[] = []

  const visit = (node: SchemaNode, parentLabel: string | null) => {
    const label = labels.get(node.id)
    if (!label) return
    blocks.push(renderResource(node, label, parentLabel))
    for (const child of node.children ?? []) visit(child, label)
  }

  visit(root, null)

  const header = [
    '# CloudPAM Schema - Terraform export',
    `# Generated ${generatedAt} by the CloudPAM Schema Planner.`,
    '#',
    '# Requires the CloudPAM Terraform provider. Configure it with the server',
    '# address and an API key, for example:',
    '#',
    '#   provider "cloudpam" {',
    '#     endpoint = "https://cloudpam.example.com"',
    '#     api_key  = var.cloudpam_api_key',
    '#   }',
    '',
    'terraform {',
    '  required_providers {',
    '    cloudpam = {',
    '      source = "BadgerOps/cloudpam"',
    '    }',
    '  }',
    '}',
  ].join('\n')

  return [header, ...blocks].join('\n\n') + '\n'
}
