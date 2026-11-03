import { toCsv } from '../../utils/csv'
import { buildApplyRequest } from './applyRequest'
import type { SchemaNode } from './cidr'
import { schemaToTerraform } from './terraform'
import { toYaml } from './yaml'

export function schemaToCsv(schema: SchemaNode): string {
  const flatten = (node: SchemaNode, parentName: string | null): string[][] => {
    const rows = [[node.name, node.cidr, node.type, parentName ?? '']]
    for (const child of node.children) {
      rows.push(...flatten(child, node.name))
    }
    return rows
  }
  return toCsv(['name', 'cidr', 'type', 'parent_name'], flatten(schema, null))
}

export function schemaToJson(schema: SchemaNode): string {
  return JSON.stringify(buildApplyRequest(schema), null, 2) + '\n'
}

export function schemaToYaml(schema: SchemaNode, generatedAt: string): string {
  return [
    `# CloudPAM Schema Planner export. Generated ${generatedAt}.`,
    '# This is the request body for POST /api/v1/schema/apply.',
    toYaml(buildApplyRequest(schema)),
  ].join('\n')
}

export interface ExportSpec {
  label: string
  filename: string
  mime: string
  render: (schema: SchemaNode, generatedAt: string) => string
}

export const EXPORT_FORMATS = {
  terraform: { label: 'Terraform', filename: 'cloudpam-schema.tf', mime: 'text/plain', render: schemaToTerraform },
  json: { label: 'JSON', filename: 'cloudpam-schema.json', mime: 'application/json', render: schemaToJson },
  yaml: { label: 'YAML', filename: 'cloudpam-schema.yaml', mime: 'application/yaml', render: schemaToYaml },
  csv: { label: 'CSV', filename: 'cloudpam-schema.csv', mime: 'text/csv', render: schemaToCsv },
} satisfies Record<string, ExportSpec>

export type ExportFormat = keyof typeof EXPORT_FORMATS
