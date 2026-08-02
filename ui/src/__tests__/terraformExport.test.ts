import { describe, expect, it } from 'vitest'
import { schemaToTerraform } from '../wizard/utils/terraform'
import type { SchemaNode } from '../wizard/utils/cidr'

const GENERATED_AT = '2026-08-02T00:00:00.000Z'

function node(id: string, name: string, cidr: string, type: SchemaNode['type'], children: SchemaNode[] = []): SchemaNode {
  return { id, name, cidr, type, children }
}

describe('schemaToTerraform', () => {
  // Regression: the Terraform export wrote a "# TODO: Implement Terraform
  // export" placeholder, so the button produced a file that could never apply.
  it('emits real cloudpam_pool resources rather than a placeholder', () => {
    const schema = node('1', 'Global', '10.0.0.0/8', 'supernet')
    const tf = schemaToTerraform(schema, GENERATED_AT)

    expect(tf).not.toContain('TODO')
    expect(tf).toContain('resource "cloudpam_pool"')
    expect(tf).toContain('name = "Global"')
    expect(tf).toContain('cidr = "10.0.0.0/8"')
    expect(tf).toContain('type = "supernet"')
    expect(tf).toContain('required_providers')
  })

  it('links children to parents by resource reference, not literal id', () => {
    const schema = node('1', 'Global', '10.0.0.0/8', 'supernet', [
      node('2', 'us-east-1', '10.0.0.0/12', 'region', [
        node('3', 'prod', '10.0.0.0/16', 'environment'),
      ]),
    ])
    const tf = schemaToTerraform(schema, GENERATED_AT)

    expect(tf).toContain('parent_id = cloudpam_pool.global.id')
    expect(tf).toContain('parent_id = cloudpam_pool.us-east-1.id')
    // The root has no parent.
    expect(tf.match(/parent_id/g)).toHaveLength(2)
  })

  it('gives colliding pool names distinct resource labels', () => {
    const schema = node('1', 'root', '10.0.0.0/8', 'supernet', [
      node('2', 'prod', '10.0.0.0/16', 'environment'),
      node('3', 'prod', '10.1.0.0/16', 'environment'),
    ])
    const tf = schemaToTerraform(schema, GENERATED_AT)

    expect(tf).toContain('resource "cloudpam_pool" "prod"')
    expect(tf).toContain('resource "cloudpam_pool" "prod_2"')
  })

  // Pool names are user input and flow into HCL, so interpolation markers and
  // quotes must not escape their string literal.
  it('neutralises HCL interpolation and quotes in user-supplied names', () => {
    const schema = node('1', 'evil${file("/etc/passwd")}"', '10.0.0.0/8', 'supernet')
    const tf = schemaToTerraform(schema, GENERATED_AT)

    // `$${` is HCL's escape for a literal `${`, so the interpolation is inert.
    expect(tf).toContain('$${')
    // No *unescaped* `${` survives anywhere in the output.
    expect(tf).not.toMatch(/(?<!\$)\$\{/)
    // The embedded quote is escaped, not left to close the literal early.
    expect(tf).toContain('\\"')
  })

  it('produces a valid label for a name with no usable identifier characters', () => {
    const schema = node('1', '///', '10.0.0.0/8', 'supernet')
    const tf = schemaToTerraform(schema, GENERATED_AT)

    // hclIdent falls back rather than emitting an invalid label.
    expect(tf).toMatch(/resource "cloudpam_pool" "[A-Za-z_][A-Za-z0-9_-]*"/)
  })

  it('records when and by what the file was generated', () => {
    const tf = schemaToTerraform(node('1', 'root', '10.0.0.0/8', 'supernet'), GENERATED_AT)
    expect(tf).toContain(GENERATED_AT)
    expect(tf).toContain('Schema Planner')
  })
})
