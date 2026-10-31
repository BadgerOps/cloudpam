import type { ReactNode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NetworkObjectTable } from '../pages/DiscoveryPage'
import { ApiRequestError } from '../api/client'
import { ToastContext, useToastState } from '../hooks/useToast'
import ToastContainer from '../components/ToastContainer'
import type { NetworkObject } from '../api/types'

const object: NetworkObject = {
  id: 42,
  object_type: 'vpc',
  provider: 'aws',
  account_id: 1,
  name: 'prod-vpc',
  cidr: '10.0.0.0/16',
  state: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

function ToastHarness({ children }: { children: ReactNode }) {
  const toast = useToastState()
  return (
    <ToastContext.Provider value={toast}>
      {children}
      <ToastContainer />
    </ToastContext.Provider>
  )
}

function renderTable(props: Partial<Parameters<typeof NetworkObjectTable>[0]> = {}) {
  return render(
    <ToastHarness>
      <NetworkObjectTable
        objects={[object]}
        loading={false}
        accounts={[]}
        pools={[]}
        onShowRelationships={vi.fn()}
        {...props}
      />
    </ToastHarness>,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('NetworkObjectTable delete action', () => {
  it('hides the delete button without canDelete', () => {
    renderTable({ onDelete: vi.fn() })
    expect(screen.queryByLabelText('Delete network object prod-vpc')).toBeNull()
  })

  it('shows the delete button with canDelete', () => {
    renderTable({ canDelete: true, onDelete: vi.fn() })
    expect(screen.getByLabelText('Delete network object prod-vpc')).toBeTruthy()
  })

  it('names the object and CIDR in the confirmation and does nothing when cancelled', () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const onDelete = vi.fn()
    renderTable({ canDelete: true, onDelete })

    fireEvent.click(screen.getByLabelText('Delete network object prod-vpc'))

    expect(confirmSpy).toHaveBeenCalledTimes(1)
    const message = confirmSpy.mock.calls[0][0] as string
    expect(message).toContain('Delete "prod-vpc" (10.0.0.0/16)?')
    expect(message).toContain('not deleted')
    expect(onDelete).not.toHaveBeenCalled()
  })

  it('calls delete once and disables the button while pending, then refreshes', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    let resolveDelete: () => void = () => {}
    const onDelete = vi.fn(
      () => new Promise<void>((resolve) => {
        resolveDelete = resolve
      }),
    )
    const onDeleted = vi.fn()
    renderTable({ canDelete: true, onDelete, onDeleted })

    const button = screen.getByLabelText('Delete network object prod-vpc') as HTMLButtonElement
    fireEvent.click(button)
    await waitFor(() => expect(button.disabled).toBe(true))
    fireEvent.click(button)

    expect(onDelete).toHaveBeenCalledTimes(1)
    expect(onDelete).toHaveBeenCalledWith(object)
    expect(onDeleted).not.toHaveBeenCalled()

    resolveDelete()

    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1))
    expect(screen.getByText('Deleted network object prod-vpc')).toBeTruthy()
  })

  it('shows the dependency message when the server returns 409', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const onDelete = vi.fn().mockRejectedValue(
      new ApiRequestError(409, { error: 'conflict: network object has 2 child objects' }),
    )
    const onDeleted = vi.fn()
    renderTable({ canDelete: true, onDelete, onDeleted })

    const button = screen.getByLabelText('Delete network object prod-vpc') as HTMLButtonElement
    fireEvent.click(button)

    await waitFor(() => {
      expect(
        screen.getByText(
          'Remove or resolve its child objects and relationships first. conflict: network object has 2 child objects',
        ),
      ).toBeTruthy()
    })
    expect(onDeleted).not.toHaveBeenCalled()
    expect(button.disabled).toBe(false)
  })

  it('shows the server message for other errors', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const onDelete = vi.fn().mockRejectedValue(new ApiRequestError(404, { error: 'network object not found' }))
    renderTable({ canDelete: true, onDelete })

    fireEvent.click(screen.getByLabelText('Delete network object prod-vpc'))

    await waitFor(() => expect(screen.getByText('network object not found')).toBeTruthy())
  })
})
