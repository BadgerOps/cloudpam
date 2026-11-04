import { describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import DiscoveryWizard from '../components/DiscoveryWizard'
import type { Account } from '../api/types'

vi.mock('../hooks/useDiscovery', () => ({
  useAgentProvisioning: () => ({
    provision: vi.fn(async (name: string) => ({
      agent_name: name,
      api_key: 'cpk_test',
      api_key_id: 'key-1',
      server_url: 'https://pam.example.com',
      token: 'boot-token',
    })),
    loading: false,
    error: null,
  }),
  useDiscoveryAgents: () => ({ agents: [], fetch: vi.fn() }),
}))
const { createAccount } = vi.hoisted(() => ({
  createAccount: vi.fn(async (req: { key: string; name: string; provider?: string }) => ({
    id: 99, key: req.key, name: req.name, provider: req.provider, regions: [],
  })),
}))
vi.mock('../hooks/useAccounts', () => ({ useAccounts: () => ({ createAccount }) }))
vi.mock('../hooks/useToast', () => ({ useToast: () => ({ showToast: vi.fn() }) }))
vi.mock('../components/ConnectivityBadge', () => ({ default: () => null }))

const accounts = [
  { id: 1, key: 'aws:111', name: 'AWS Prod', provider: 'aws', regions: [] },
  { id: 2, key: 'gcp:net-prod', name: 'GCP Net', provider: 'gcp', external_id: 'net-prod', regions: [] },
  { id: 3, key: 'gcp:shared-vpc', name: 'GCP Shared', provider: 'gcp', external_id: 'shared-vpc', regions: [] },
] as unknown as Account[]

function renderWizard(list: Account[] = accounts) {
  return render(<DiscoveryWizard accounts={list} onAccountCreated={vi.fn()} onClose={vi.fn()} />)
}

function projectInput() {
  return screen.getByLabelText('GCP project ID') as HTMLInputElement
}

function nextButton() {
  return screen.getByRole('button', { name: /next/i }) as HTMLButtonElement
}

describe('DiscoveryWizard provider choice', () => {
  it('lists only the chosen provider accounts and relabels the modes', () => {
    renderWizard()
    expect(screen.getByRole('option', { name: /AWS Prod/ })).toBeTruthy()
    expect(screen.queryByRole('option', { name: /GCP Net/ })).toBeNull()
    expect(screen.getByText('AWS Organization')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('GCP'))

    expect(screen.getByRole('option', { name: /GCP Net/ })).toBeTruthy()
    expect(screen.queryByRole('option', { name: /AWS Prod/ })).toBeNull()
    expect(screen.getByText('Single Project')).toBeTruthy()
    expect(screen.getByText('GCP Organization or Folder')).toBeTruthy()
  })

  it('requires a valid parent before leaving step 1 in gcp org mode', () => {
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.click(screen.getByText('GCP Organization or Folder'))
    expect(nextButton().disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Organization or folder'), { target: { value: 'projects/12' } })
    expect(nextButton().disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Organization or folder'), { target: { value: 'folders/12' } })
    expect(nextButton().disabled).toBe(false)
  })

  it('generates gcp configs under a gcp tab strip', async () => {
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.change(screen.getByLabelText('GCP project ID'), { target: { value: 'override-project' } })
    fireEvent.click(nextButton())
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Provision Agent' }))
    })
    fireEvent.click(nextButton())

    const tabs = ['Shell', 'YAML', 'Terraform', 'Docker', 'IAM Setup']
      .filter(label => screen.queryByRole('button', { name: label }))
    expect(tabs).toEqual(['Shell', 'YAML', 'Docker', 'IAM Setup'])
    expect(screen.getByText(/export CLOUDPAM_GCP_PROJECT='override-project'/)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'IAM Setup' }))
    expect(screen.getByText(/gcloud projects add-iam-policy-binding 'override-project'/)).toBeTruthy()
  })
})

describe('DiscoveryWizard state hygiene', () => {
  it('clears the gcp project override when the provider or account changes', () => {
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.change(projectInput(), { target: { value: 'override-project' } })
    fireEvent.change(screen.getByRole('combobox'), { target: { value: '3' } })
    expect(projectInput().value).toBe('')
    expect(projectInput().placeholder).toBe('shared-vpc')

    fireEvent.change(projectInput(), { target: { value: 'override-project' } })
    fireEvent.click(screen.getByLabelText('AWS'))
    fireEvent.click(screen.getByLabelText('GCP'))
    expect(projectInput().value).toBe('')
  })

  it('creates the new account under the wizard provider, which the form shows read-only', async () => {
    createAccount.mockClear()
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.click(screen.getByRole('button', { name: '+ Create new account' }))
    expect(screen.queryAllByRole('combobox')).toHaveLength(0)
    expect(screen.getByText('GCP', { selector: '[data-testid="new-account-provider"]' })).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Key'), { target: { value: 'gcp:new' } })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'GCP New' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create Account' }))
    })
    expect(createAccount).toHaveBeenCalledWith(expect.objectContaining({ provider: 'gcp' }))
    expect(nextButton().disabled).toBe(false)
  })

  it('does not carry a created account into another provider', async () => {
    const gcpOnly = accounts.filter(a => a.provider === 'gcp')
    renderWizard(gcpOnly)
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.click(screen.getByRole('button', { name: '+ Create new account' }))
    fireEvent.change(screen.getByLabelText('Key'), { target: { value: 'gcp:new' } })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'GCP New' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create Account' }))
    })
    expect(screen.getByText('Server connectivity')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('AWS'))
    expect(screen.queryByText('Server connectivity')).toBeNull()
    expect(nextButton().disabled).toBe(true)
  })

  it('blocks Next on an impersonation template the agent would reject', () => {
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.click(screen.getByText('GCP Organization or Folder'))
    fireEvent.change(screen.getByLabelText('Organization or folder'), { target: { value: 'folders/12' } })
    const impersonate = screen.getByLabelText('Impersonate service account (optional)')

    fireEvent.change(impersonate, { target: { value: 'disc-{project}@hub.iam.gserviceaccount.com' } })
    expect(screen.getByText(/6 to 30 lowercase letters/)).toBeTruthy()
    expect(nextButton().disabled).toBe(true)

    fireEvent.change(impersonate, { target: { value: 'cloudpam-discovery@{project}.iam.gserviceaccount.com' } })
    expect(screen.queryByText(/6 to 30 lowercase letters/)).toBeNull()
    expect(nextButton().disabled).toBe(false)

    fireEvent.change(impersonate, { target: { value: '' } })
    expect(nextButton().disabled).toBe(false)
  })
})
