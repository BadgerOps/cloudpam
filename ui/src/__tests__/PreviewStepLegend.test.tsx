import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import PreviewStep from '../wizard/steps/PreviewStep'
import type { SchemaNode } from '../wizard/utils/cidr'
import { POOL_TYPES } from '../utils/poolTypes'

const schema: SchemaNode = {
  id: 'root',
  name: 'Corporate Supernet',
  type: 'supernet',
  cidr: '10.0.0.0/8',
  children: [],
}

describe('PreviewStep legend', () => {
  it('lists every legend-visible pool type with its assigned color', () => {
    const { container } = render(
      <PreviewStep
        schema={schema}
        conflicts={[]}
        conflictsLoading={false}
        conflictsError={null}
        onExport={() => {}}
      />,
    )
    for (const t of POOL_TYPES) {
      expect(screen.getByText(t.label)).toBeTruthy()
      expect(container.querySelector('.' + t.dot)).toBeTruthy()
    }
  })

  it('includes Subnet, which the old literal legend omitted', () => {
    render(
      <PreviewStep
        schema={schema}
        conflicts={[]}
        conflictsLoading={false}
        conflictsError={null}
        onExport={() => {}}
      />,
    )
    expect(screen.getByText('Subnet')).toBeTruthy()
  })
})
