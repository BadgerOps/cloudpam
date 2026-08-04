import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import StatusBadge from '../components/StatusBadge'

describe('StatusBadge', () => {
  it('gives the type variant a dark-mode text colour', () => {
    // The type variant is the one primitive that renders pool-type labels.
    // Without a dark: text class it inherits text-gray-600 on a dark surface.
    const { container } = render(<StatusBadge label="vpc" variant="type" />)
    const badge = container.firstElementChild as HTMLElement

    expect(badge.className).toMatch(/\bdark:text-/)
  })

  it('still renders the pool-type dot from the shared colour source', () => {
    const { container } = render(<StatusBadge label="vpc" variant="type" />)

    expect(container.querySelector('.bg-amber-500')).toBeTruthy()
  })

  it('renders nothing for an empty label', () => {
    const { container } = render(<StatusBadge label="" />)

    expect(container.firstElementChild).toBeNull()
  })
})
