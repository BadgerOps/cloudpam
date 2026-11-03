import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import Pager from '../components/Pager'

describe('Pager', () => {
  it('shows the range for the current page and steps forward and back', () => {
    const onPageChange = vi.fn()
    render(<Pager page={2} pageSize={50} total={120} noun="blocks" onPageChange={onPageChange} />)

    expect(screen.getByText('Showing 51-100 of 120 blocks')).toBeTruthy()
    fireEvent.click(screen.getByText('Next'))
    expect(onPageChange).toHaveBeenLastCalledWith(3)
    fireEvent.click(screen.getByText('Previous'))
    expect(onPageChange).toHaveBeenLastCalledWith(1)
  })

  it('disables Previous on the first page and Next on the last page', () => {
    const { rerender } = render(
      <Pager page={1} pageSize={50} total={120} noun="items" onPageChange={() => {}} />,
    )
    expect((screen.getByText('Previous') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByText('Next') as HTMLButtonElement).disabled).toBe(false)

    rerender(<Pager page={3} pageSize={50} total={120} noun="items" onPageChange={() => {}} />)
    expect(screen.getByText('Showing 101-120 of 120 items')).toBeTruthy()
    expect((screen.getByText('Previous') as HTMLButtonElement).disabled).toBe(false)
    expect((screen.getByText('Next') as HTMLButtonElement).disabled).toBe(true)
  })

  it('steps back to the last page when the requested page is past the end', () => {
    const onPageChange = vi.fn()
    render(<Pager page={4} pageSize={50} total={120} noun="items" onPageChange={onPageChange} />)
    expect(onPageChange).toHaveBeenCalledWith(3)
  })

  it('renders nothing when there are no items', () => {
    const { container } = render(
      <Pager page={1} pageSize={50} total={0} noun="items" onPageChange={() => {}} />,
    )
    expect(container.firstChild).toBeNull()
  })
})
