import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Toggle } from '../components/primitives'

describe('Toggle', () => {
  it('is a switch whose accessible name carries its state', () => {
    const { rerender } = render(<Toggle checked={false} onChange={() => {}} label="Local auth" />)

    const off = screen.getByRole('switch', { name: 'Local auth, off' })
    expect(off.getAttribute('aria-checked')).toBe('false')

    rerender(<Toggle checked onChange={() => {}} label="Local auth" />)
    expect(screen.getByRole('switch', { name: 'Local auth, on' }).getAttribute('aria-checked')).toBe('true')
  })

  it('reports the flipped value from the switch and from its label text', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} onChange={onChange} label="Local auth" hint="Allow password login" />)

    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByText('Local auth'))
    expect(onChange).toHaveBeenCalledTimes(2)
    expect(screen.getByText('Allow password login')).toBeTruthy()
  })

  it('does nothing while disabled', () => {
    const onChange = vi.fn()
    render(<Toggle checked disabled onChange={onChange} label="Local auth" />)

    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).not.toHaveBeenCalled()
  })
})
