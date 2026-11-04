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
vi.mock('../hooks/useAccounts', () => ({ useAccounts: () => ({ createAccount: vi.fn() }) }))
vi.mock('../hooks/useToast', () => ({ useToast: () => ({ showToast: vi.fn() }) }))
vi.mock('../components/ConnectivityBadge', () => ({ default: () => null }))

const accounts = [
  { id: 1, key: 'aws:111', name: 'AWS Prod', provider: 'aws', regions: [] },
  { id: 2, key: 'gcp:net-prod', name: 'GCP Net', provider: 'gcp', external_id: 'net-prod', regions: [] },
] as unknown as Account[]

function renderWizard() {
  return render(<DiscoveryWizard accounts={accounts} onAccountCreated={vi.fn()} onClose={vi.fn()} />)
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
