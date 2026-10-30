import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountsPage from '../pages/AccountsPage'
import { ApiRequestError } from '../api/client'
import type { Account } from '../api/types'

const mockUseAccounts = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAccounts', () => ({
  useAccounts: () => mockUseAccounts(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

const prod: Account = {
  id: 1,
  key: 'aws:111111111111',
  name: 'prod-platform',
  provider: 'aws',
  external_id: '111111111111',
  description: 'Core production',
  platform: 'eks',
  tier: 'prd',
  environment: 'production',
  regions: ['us-east-1'],
  created_at: '2026-01-01T00:00:00Z',
}

const accounts: Account[] = [
  prod,
  { id: 2, key: 'gcp:staging-net', name: 'staging-net', provider: 'gcp', created_at: '2026-01-01T00:00:00Z' },
]

const fetchAccounts = vi.fn()
const createAccount = vi.fn()
const updateAccount = vi.fn()
const deleteAccount = vi.fn()
const showToast = vi.fn()

function renderPage(path = '/accounts') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AccountsPage />
    </MemoryRouter>,
  )
}

function field(scope: ReturnType<typeof within>, label: string) {
  return scope.getByLabelText(label) as HTMLInputElement | HTMLSelectElement
}

beforeEach(() => {
  vi.clearAllMocks()
  mockUseToast.mockReturnValue({ showToast })
  mockUseAccounts.mockReturnValue({
    accounts,
    loading: false,
    error: null,
    fetchAccounts,
    createAccount,
    updateAccount,
    deleteAccount,
  })
})

describe('AccountsPage deep link', () => {
  it('narrows the list to the account named in ?selected', async () => {
    renderPage('/accounts?selected=2')

    await waitFor(() => {
      expect(screen.getByRole('cell', { name: 'staging-net' })).toBeTruthy()
      expect(screen.queryByRole('cell', { name: 'prod-platform' })).toBeNull()
    })
  })
})

describe('AccountsPage edit', () => {
  it('opens the account pre-filled with the key read-only', () => {
    renderPage()
    const row = screen.getByRole('row', { name: /prod-platform/ })
    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Edit Account' }))
    const key = field(dialog, 'Key (required)') as HTMLInputElement
    expect(key.value).toBe('aws:111111111111')
    expect(key.readOnly).toBe(true)
    expect(field(dialog, 'Name (required)').value).toBe('prod-platform')
    expect(field(dialog, 'Provider').value).toBe('aws')
    expect(field(dialog, 'Tier').value).toBe('prd')
    expect(field(dialog, 'Environment').value).toBe('production')
    expect(field(dialog, 'Platform').value).toBe('eks')
    expect(field(dialog, 'Regions').value).toBe('us-east-1')
    expect(field(dialog, 'External ID').value).toBe('111111111111')
    expect(field(dialog, 'Description').value).toBe('Core production')
  })

  it('sends a PATCH carrying the changed fields and keeping the untouched ones', async () => {
    updateAccount.mockResolvedValue({ ...prod, name: 'prod-renamed', regions: ['us-east-1', 'us-west-2'] })
    renderPage()
    fireEvent.click(within(screen.getByRole('row', { name: /prod-platform/ })).getByRole('button', { name: 'Edit' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Edit Account' }))
    fireEvent.change(field(dialog, 'Name (required)'), { target: { value: 'prod-renamed' } })
    fireEvent.change(field(dialog, 'Regions'), { target: { value: 'us-east-1, us-west-2,' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateAccount).toHaveBeenCalledTimes(1))
    expect(updateAccount).toHaveBeenCalledWith(1, {
      name: 'prod-renamed',
      provider: 'aws',
      external_id: '111111111111',
      description: 'Core production',
      platform: 'eks',
      tier: 'prd',
      environment: 'production',
      regions: ['us-east-1', 'us-west-2'],
    })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('clears regions when the field is emptied', async () => {
    updateAccount.mockResolvedValue({ ...prod, regions: [] })
    renderPage()
    fireEvent.click(within(screen.getByRole('row', { name: /prod-platform/ })).getByRole('button', { name: 'Edit' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Edit Account' }))
    fireEvent.change(field(dialog, 'Regions'), { target: { value: '' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateAccount).toHaveBeenCalledTimes(1))
    expect(updateAccount.mock.calls[0][1].regions).toEqual([])
  })
})

describe('AccountsPage create', () => {
  it('submits every writable field', async () => {
    createAccount.mockResolvedValue({ ...prod, id: 3 })
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: 'New Account' }))

    const form = within(screen.getByRole('form', { name: 'New Account' }))
    fireEvent.change(field(form, 'Key (required)'), { target: { value: 'gcp:new-project-1' } })
    fireEvent.change(field(form, 'Name (required)'), { target: { value: 'new-project' } })
    fireEvent.change(field(form, 'Provider'), { target: { value: 'gcp' } })
    fireEvent.change(field(form, 'Tier'), { target: { value: 'stg' } })
    fireEvent.change(field(form, 'Environment'), { target: { value: 'staging' } })
    fireEvent.change(field(form, 'Platform'), { target: { value: 'gke' } })
    fireEvent.change(field(form, 'Regions'), { target: { value: 'us-central1 , europe-west1' } })
    fireEvent.change(field(form, 'External ID'), { target: { value: 'new-project-1' } })
    fireEvent.change(field(form, 'Description'), { target: { value: 'Staging GKE' } })
    fireEvent.click(form.getByRole('button', { name: 'Create Account' }))

    await waitFor(() => expect(createAccount).toHaveBeenCalledTimes(1))
    expect(createAccount).toHaveBeenCalledWith({
      key: 'gcp:new-project-1',
      name: 'new-project',
      provider: 'gcp',
      external_id: 'new-project-1',
      description: 'Staging GKE',
      platform: 'gke',
      tier: 'stg',
      environment: 'staging',
      regions: ['us-central1', 'europe-west1'],
    })
  })

  it('shows a duplicate key refusal inside the form and keeps it open', async () => {
    createAccount.mockRejectedValue(
      new ApiRequestError(409, { error: 'account key already exists', detail: 'aws:111111111111' }),
    )
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: 'New Account' }))

    const form = within(screen.getByRole('form', { name: 'New Account' }))
    fireEvent.change(field(form, 'Key (required)'), { target: { value: 'aws:111111111111' } })
    fireEvent.change(field(form, 'Name (required)'), { target: { value: 'dup' } })
    fireEvent.click(form.getByRole('button', { name: 'Create Account' }))

    const alert = await form.findByRole('alert')
    expect(alert.textContent).toContain('account key already exists')
    expect(field(form, 'Key (required)').value).toBe('aws:111111111111')
  })
})
