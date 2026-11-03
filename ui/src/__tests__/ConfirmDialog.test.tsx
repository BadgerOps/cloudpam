import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConfirmDialog } from '../components/primitives'

afterEach(() => {
  document.body.style.overflow = ''
})

describe('ConfirmDialog', () => {
  it('runs the action only from the confirm button and closes from Cancel', () => {
    const onConfirm = vi.fn()
    const onClose = vi.fn()
    render(
      <ConfirmDialog open title="Delete key" confirmLabel="Delete" tone="danger" onConfirm={onConfirm} onClose={onClose}>
        This cannot be undone.
      </ConfirmDialog>,
    )

    expect(screen.getByRole('dialog', { name: 'Delete key' })).toBeTruthy()
    expect(screen.getByText('This cannot be undone.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('renders nothing while closed', () => {
    render(
      <ConfirmDialog open={false} title="Delete key" confirmLabel="Delete" onConfirm={() => {}} onClose={() => {}}>
        body
      </ConfirmDialog>,
    )

    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
