import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SchemaPlanner from '../wizard/SchemaPlanner'

const mockUseApplySchema = vi.hoisted(() => vi.fn())

vi.mock('../wizard/hooks/useApplySchema', () => ({
  useApplySchema: () => mockUseApplySchema(),
}))

function idle() {
  return { result: null, loading: false, error: null, apply: vi.fn() }
}

function expectOnlyPageTitle() {
  expect(screen.queryByRole('banner')).toBeNull()
  expect(screen.queryByRole('main')).toBeNull()
  expect(screen.getAllByRole('heading', { level: 1 }).map((h) => h.textContent)).toEqual(['Schema Planner'])
}

describe('SchemaPlanner page chrome', () => {
  beforeEach(() => {
    mockUseApplySchema.mockReturnValue(idle())
  })

  it('shows one page title and no app header while planning', () => {
    render(<SchemaPlanner />)
    screen.getByRole('button', { name: 'Continue' })
    expectOnlyPageTitle()
  })

  it('shows the same chrome after the schema is applied', () => {
    mockUseApplySchema.mockReturnValue({ ...idle(), result: { created: 3, root_pool_id: 7, errors: [] } })
    render(<SchemaPlanner />)
    screen.getByText('Schema Applied Successfully')
    expectOnlyPageTitle()
  })
})
