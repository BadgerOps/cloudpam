import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { EmptyState } from '../components/primitives'

describe('EmptyState', () => {
  it('explains the feature and offers the first action', () => {
    const onClick = vi.fn()
    render(
      <EmptyState
        title="No API keys"
        description="Keys let automation call the API without a session."
        action={{ label: 'Create key', onClick }}
      />,
    )

    expect(screen.getByText('No API keys')).toBeTruthy()
    expect(screen.getByText('Keys let automation call the API without a session.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders without a description or action', () => {
    render(<EmptyState title="No matches" />)

    expect(screen.getByText('No matches')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })
})
