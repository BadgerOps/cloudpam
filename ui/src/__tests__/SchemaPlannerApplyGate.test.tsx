import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SchemaPlanner from '../wizard/SchemaPlanner'
import type { SchemaNode } from '../wizard/utils/cidr'

const mocks = vi.hoisted(() => ({
  generated: { schema: null as unknown, error: null as string | null },
  apply: vi.fn(),
}))

vi.mock('../wizard/hooks/useSchemaGenerator', () => ({
  useSchemaGenerator: () => mocks.generated,
}))

vi.mock('../wizard/hooks/useConflictChecker', () => ({
  useConflictChecker: () => ({ conflicts: [], loading: false, error: null }),
}))

vi.mock('../wizard/hooks/useApplySchema', () => ({
  useApplySchema: () => ({ result: null, loading: false, error: null, apply: mocks.apply }),
}))

const node = (id: string, name: string, cidr: string, children: SchemaNode[] = []): SchemaNode => ({
  id,
  name,
  type: id === 'root' ? 'supernet' : 'region',
  cidr,
  children,
})

function openPreview() {
  render(<SchemaPlanner />)
  fireEvent.click(screen.getByRole('button', { name: /Enterprise Multi-Region/ }))
  for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
}

describe('SchemaPlanner apply gate', () => {
  beforeEach(() => {
    localStorage.clear()
    mocks.apply.mockReset()
  })

  it('applies a plan whose children all sit inside their parents', () => {
    mocks.generated = { schema: node('root', 'Root', '10.0.0.0/8', [node('r1', 'us-east-1', '10.0.0.0/12')]), error: null }
    openPreview()
    expect(screen.queryByRole('alert')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Apply to CloudPAM/ }))
    expect(mocks.apply).toHaveBeenCalledTimes(1)
  })

  it('names a child outside its parent and refuses to apply', () => {
    mocks.generated = {
      schema: node('root', 'Root', '10.0.0.0/16', [node('r1', 'us-east-1', '10.0.0.0/12')]),
      error: null,
    }
    openPreview()
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain('us-east-1')
    expect(alert.textContent).toContain('10.0.0.0/12')
    fireEvent.click(screen.getByRole('button', { name: /Apply to CloudPAM/ }))
    expect(mocks.apply).not.toHaveBeenCalled()
  })

  it('shows why the template does not fit and refuses to apply', () => {
    mocks.generated = {
      schema: node('root', 'Root', '10.0.0.0/24'),
      error: '10.0.0.0/24 is too small for this strategy.',
    }
    openPreview()
    expect(within(screen.getByRole('alert')).getByText(/10\.0\.0\.0\/24 is too small/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Apply to CloudPAM/ }))
    expect(mocks.apply).not.toHaveBeenCalled()
  })
})
