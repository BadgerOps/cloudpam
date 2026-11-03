import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import PreviewStep from '../wizard/steps/PreviewStep'
import type { SchemaNode } from '../wizard/utils/cidr'

const schema: SchemaNode = {
  id: 'root',
  name: 'Corporate Supernet',
  type: 'supernet',
  cidr: '10.0.0.0/8',
  children: [],
}

describe('PreviewStep export buttons', () => {
  it('offers Terraform, JSON, YAML and CSV and reports the chosen format', () => {
    const onExport = vi.fn()
    render(
      <PreviewStep
        schema={schema}
        problems={[]}
        conflicts={[]}
        conflictsLoading={false}
        conflictsError={null}
        onExport={onExport}
      />,
    )
    const expected = [
      ['Export Terraform', 'terraform'],
      ['Export JSON', 'json'],
      ['Export YAML', 'yaml'],
      ['Export CSV', 'csv'],
    ]
    for (const [label, format] of expected) {
      fireEvent.click(screen.getByRole('button', { name: label }))
      expect(onExport).toHaveBeenLastCalledWith(format)
    }
    expect(onExport).toHaveBeenCalledTimes(expected.length)
  })
})
