import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import PoolTree from '../components/PoolTree'
import type { PoolWithStats } from '../api/types'

function pool(id: number, name: string, cidr: string, children: PoolWithStats[] = []): PoolWithStats {
  return {
    id,
    name,
    cidr,
    type: 'supernet',
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
    children,
  } as PoolWithStats
}

// root -> region -> environment, so there is a node that starts expanded
// (depth 0) and a node that starts collapsed (depth 1).
const nodes: PoolWithStats[] = [
  pool(1, 'Root', '10.0.0.0/8', [
    pool(2, 'Region', '10.0.0.0/12', [pool(3, 'Environment', '10.0.0.0/16')]),
  ]),
]

function renderTree() {
  return render(<PoolTree nodes={nodes} onSelect={vi.fn()} />)
}

describe('PoolTree global expand controls', () => {
  it('renders the first level expanded and deeper levels collapsed by default', () => {
    renderTree()

    expect(screen.getByText('Root')).toBeTruthy()
    expect(screen.getByText('Region')).toBeTruthy()
    expect(screen.queryByText('Environment')).toBeNull()
  })

  it('Expand All reveals every descendant', () => {
    renderTree()

    fireEvent.click(screen.getByRole('button', { name: 'Expand All' }))

    expect(screen.getByText('Region')).toBeTruthy()
    expect(screen.getByText('Environment')).toBeTruthy()
  })

  it('Collapse All collapses the initially expanded root node', () => {
    renderTree()

    fireEvent.click(screen.getByRole('button', { name: 'Collapse All' }))

    expect(screen.getByText('Root')).toBeTruthy()
    expect(screen.queryByText('Region')).toBeNull()
    expect(screen.queryByText('Environment')).toBeNull()
  })

  it('Collapse All still collapses after an Expand All', () => {
    renderTree()

    fireEvent.click(screen.getByRole('button', { name: 'Expand All' }))
    expect(screen.getByText('Environment')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Collapse All' }))
    expect(screen.queryByText('Region')).toBeNull()
  })

  it('reapplies Collapse All after a manual expand of the same node', () => {
    renderTree()

    fireEvent.click(screen.getByRole('button', { name: 'Collapse All' }))
    expect(screen.queryByText('Region')).toBeNull()

    // The root's chevron is the only toggle rendered while collapsed.
    const toggles = screen.getAllByRole('button').filter(b => b.textContent === '')
    fireEvent.click(toggles[0])
    expect(screen.getByText('Region')).toBeTruthy()

    // A second Collapse All must reach the node the user re-expanded by hand.
    fireEvent.click(screen.getByRole('button', { name: 'Collapse All' }))
    expect(screen.queryByText('Region')).toBeNull()
  })

  it('reapplies Expand All after a manual collapse', () => {
    renderTree()

    fireEvent.click(screen.getByRole('button', { name: 'Expand All' }))
    expect(screen.getByText('Environment')).toBeTruthy()

    const toggles = screen.getAllByRole('button').filter(b => b.textContent === '')
    fireEvent.click(toggles[0])
    expect(screen.queryByText('Region')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Expand All' }))
    expect(screen.getByText('Region')).toBeTruthy()
    expect(screen.getByText('Environment')).toBeTruthy()
  })
})
