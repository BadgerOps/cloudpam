import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Modal } from '../components/primitives'

function ControlledForm() {
  const [open, setOpen] = useState(true)
  const [first, setFirst] = useState('')
  const [second, setSecond] = useState('')
  return (
    <Modal open={open} title="Edit role" onClose={() => setOpen(false)}>
      <input aria-label="First" value={first} onChange={e => setFirst(e.target.value)} />
      <input aria-label="Second" value={second} onChange={e => setSecond(e.target.value)} />
    </Modal>
  )
}

function renderModal(open: boolean, onClose = vi.fn()) {
  const utils = render(
    <>
      <button>outside</button>
      <Modal open={open} title="Edit role" description="Changes apply on save." onClose={onClose}>
        <input aria-label="Name" />
        <button>Second</button>
      </Modal>
    </>,
  )
  return { ...utils, onClose }
}

afterEach(() => {
  document.body.style.overflow = ''
})

describe('Modal', () => {
  it('renders nothing while closed', () => {
    renderModal(false)

    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('exposes the title and description to assistive tech', () => {
    renderModal(true)

    const dialog = screen.getByRole('dialog', { name: 'Edit role' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(dialog.getAttribute('aria-describedby')).toBeTruthy()
    expect(screen.getByText('Changes apply on save.')).toBeTruthy()
  })

  it('closes on Escape, backdrop click, and the close button, but not on a panel click', () => {
    const { onClose } = renderModal(true)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(3)

    fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).toHaveBeenCalledTimes(3)
  })

  it('locks page scroll while open and releases it on close', () => {
    const { rerender } = renderModal(true)
    expect(document.body.style.overflow).toBe('hidden')

    rerender(
      <Modal open={false} title="Edit role" onClose={() => {}}>
        <span />
      </Modal>,
    )
    expect(document.body.style.overflow).toBe('')
  })

  it('moves focus inside, traps Tab at both ends, and restores focus on close', () => {
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    outside.focus()

    const { rerender } = renderModal(true)
    const name = screen.getByLabelText('Name')
    expect(document.activeElement).toBe(name)

    const second = screen.getByRole('button', { name: 'Second' })
    second.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }))

    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(second)

    rerender(
      <>
        <button>outside</button>
        <Modal open={false} title="Edit role" onClose={() => {}}>
          <span />
        </Modal>
      </>,
    )
    expect(document.activeElement).toBe(outside)
    outside.remove()
  })

  it('keeps focus where the user is typing when the caller passes an inline onClose', () => {
    render(<ControlledForm />)
    const second = screen.getByLabelText('Second') as HTMLInputElement
    second.focus()

    fireEvent.change(second, { target: { value: 'a' } })
    fireEvent.change(second, { target: { value: 'ab' } })

    expect(second.value).toBe('ab')
    expect(document.activeElement).toBe(second)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
