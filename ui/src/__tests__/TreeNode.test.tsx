import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import TreeNode from '../wizard/components/TreeNode'
import type { SchemaNode } from '../wizard/utils/cidr'

describe('TreeNode pool-type color', () => {
  const node = (type: SchemaNode['type']): SchemaNode => ({
    id: 'n1',
    name: 'node',
    type,
    cidr: '10.0.0.0/24',
    children: [],
  })

  it('gives a subnet its own hue instead of grey', () => {
    const { container } = render(<TreeNode node={node('subnet')} />)
    expect(container.querySelector('.bg-orange-500')).toBeTruthy()
    expect(container.querySelector('.bg-gray-400')).toBeNull()
  })

  it('dims the subnet row rather than recoloring the dot', () => {
    const { container } = render(<TreeNode node={node('subnet')} />)
    expect(container.querySelector('.opacity-50')).toBeTruthy()
  })

  it('does not dim a non-subnet row', () => {
    const { container } = render(<TreeNode node={node('region')} />)
    expect(container.querySelector('.opacity-50')).toBeNull()
    expect(container.querySelector('.bg-blue-500')).toBeTruthy()
  })
})
