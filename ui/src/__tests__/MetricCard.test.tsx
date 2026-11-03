import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ReactElement } from 'react'
import { describe, expect, it } from 'vitest'
import { MetricCard } from '../components/primitives'

function renderCard(ui: ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('MetricCard', () => {
  it('shows the value, its context and a link to act on it', () => {
    renderCard(
      <MetricCard
        label="Open drift"
        value="3"
        tone="critical"
        detail="1 critical, 2 warning"
        action={{ label: 'Review drift', to: '/drift' }}
      />,
    )

    const card = screen.getByRole('region', { name: 'Open drift' })
    expect(within(card).getByText('3')).toBeTruthy()
    expect(within(card).getByText('1 critical, 2 warning')).toBeTruthy()
    expect(within(card).getByRole('link', { name: 'Review drift' }).getAttribute('href')).toBe('/drift')
    expect(card.getAttribute('data-tone')).toBe('critical')
  })

  it('hides a stale value and its details while loading', () => {
    renderCard(
      <MetricCard label="Pools" value="12" detail="of 40" loading>
        <p>child list</p>
      </MetricCard>,
    )

    const card = screen.getByRole('region', { name: 'Pools' })
    expect(card.getAttribute('aria-busy')).toBe('true')
    expect(within(card).queryByText('12')).toBeNull()
    expect(within(card).queryByText('of 40')).toBeNull()
    expect(within(card).queryByText('child list')).toBeNull()
  })

  it('defaults to a neutral tone without an action', () => {
    renderCard(<MetricCard label="Accounts" value="4" />)

    const card = screen.getByRole('region', { name: 'Accounts' })
    expect(card.getAttribute('data-tone')).toBe('neutral')
    expect(within(card).queryByRole('link')).toBeNull()
  })
})
