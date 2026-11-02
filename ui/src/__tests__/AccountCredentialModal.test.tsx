import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountCredentialModal, { credentialProvider } from '../components/AccountCredentialModal'
import type { Account, AccountCredential, CredentialTestResult } from '../api/types'

const showToast = vi.fn()
vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast }),
}))

const gcpAccount: Account = { id: 7, key: 'gcp:staging-net', name: 'staging-net', provider: 'gcp', created_at: '2026-01-01T00:00:00Z' }
const awsAccount: Account = { id: 8, key: 'aws:111111111111', name: 'prod-platform', provider: 'aws', created_at: '2026-01-01T00:00:00Z' }

const storedGCP: AccountCredential = {
  account_id: 7,
  type: 'gcp_service_account',
  summary: { client_email: 'cloudpam@staging-net.iam.gserviceaccount.com', project_id: 'staging-net' },
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

interface Call { url: string; method: string; body: unknown }
const calls: Call[] = []
const fetchMock = vi.fn()
type Handler = (call: Call) => Response | Promise<Response>

function serve(handlers: Partial<Record<'GET' | 'PUT' | 'DELETE' | 'POST', Handler>>) {
  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = {
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
    }
    calls.push(call)
    const handler = handlers[call.method as keyof typeof handlers]
    if (!handler) throw new Error(`unexpected ${call.method} ${call.url}`)
    return handler(call)
  })
}

function renderModal(account: Account, canEdit = true) {
  const provider = credentialProvider(account)
  if (!provider) throw new Error('account has no credential provider')
  return render(
    <AccountCredentialModal account={account} provider={provider} canEdit={canEdit} returnFocusTo={null} onClose={() => {}} />,
  )
}

const modal = () => within(screen.getByTestId('account-credential-modal'))
const badge = () => screen.getByTestId('credential-state').textContent

describe('AccountCredentialModal', () => {
  beforeEach(() => {
    calls.length = 0
    fetchMock.mockReset()
    showToast.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('names only aws and gcp accounts as credential holders', () => {
    expect(credentialProvider({ provider: 'aws' })).toBe('aws')
    expect(credentialProvider({ provider: 'gcp' })).toBe('gcp')
    expect(credentialProvider({ provider: 'azure' })).toBeNull()
    expect(credentialProvider({})).toBeNull()
  })

  it('shows the none state from a 404 with the provider form', async () => {
    serve({ GET: () => json(404, { error: 'no credential stored for this account' }) })
    renderModal(gcpAccount)

    await waitFor(() => expect(badge()).toBe('None stored'))
    expect(modal().getByText('No credential stored. Sync runs as the server\'s host identity, or through a connected agent.')).toBeTruthy()
    expect(modal().getByLabelText('Service account key JSON (required)')).toBeTruthy()
    expect(modal().queryByRole('button', { name: 'Test' })).toBeNull()
    expect(modal().queryByRole('button', { name: 'Clear' })).toBeNull()
    expect(modal().queryByRole('alert')).toBeNull()
  })

  it('shows the AWS form fields for an aws account', async () => {
    serve({ GET: () => json(404, { error: 'no credential stored for this account' }) })
    renderModal(awsAccount)

    await waitFor(() => expect(badge()).toBe('None stored'))
    expect(modal().getByLabelText('Role ARN (required)')).toBeTruthy()
    expect(modal().getByLabelText('External ID')).toBeTruthy()
    expect(modal().queryByLabelText('Service account key JSON (required)')).toBeNull()
  })

  it('renders the stored summary, the last test, and the Test and Clear actions', async () => {
    const failed: AccountCredential = {
      ...storedGCP,
      last_test: { tested_at: '2026-01-02T00:00:00Z', status: 'failed', error: 'token: invalid_grant (http 400)' },
    }
    serve({ GET: () => json(200, failed) })
    renderModal(gcpAccount)

    await waitFor(() => expect(badge()).toBe('Failing'))
    expect(modal().getByText('cloudpam@staging-net.iam.gserviceaccount.com')).toBeTruthy()
    expect(modal().getByText('staging-net', { selector: 'dd' })).toBeTruthy()
    const result = screen.getByTestId('credential-test-result')
    expect(result.textContent).toContain('failed')
    expect(result.textContent).toContain('token: invalid_grant (http 400)')
    expect(modal().getByRole('button', { name: 'Test' })).toBeTruthy()
    expect(modal().getByRole('button', { name: 'Clear' })).toBeTruthy()
    expect(modal().getByRole('form', { name: 'Replace credential' })).toBeTruthy()
  })

  it('is read-only without accounts:update', async () => {
    serve({ GET: () => json(200, storedGCP) })
    renderModal(gcpAccount, false)

    await waitFor(() => expect(badge()).toBe('Stored'))
    expect(modal().getByText('cloudpam@staging-net.iam.gserviceaccount.com')).toBeTruthy()
    expect(modal().queryByRole('form')).toBeNull()
    expect(modal().queryByRole('button', { name: 'Test' })).toBeNull()
    expect(modal().queryByRole('button', { name: 'Clear' })).toBeNull()
  })

  it('sends the PUT body for the provider and shows the saved summary', async () => {
    serve({
      GET: () => json(404, { error: 'no credential stored for this account' }),
      PUT: () => json(200, storedGCP),
    })
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('None stored'))

    fireEvent.change(modal().getByLabelText('Service account key JSON (required)'), { target: { value: '{"type":"service_account"}' } })
    fireEvent.click(modal().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(badge()).toBe('Stored'))
    const put = calls.find(c => c.method === 'PUT')
    expect(put?.url).toBe('/api/v1/accounts/7/credentials')
    expect(put?.body).toEqual({ type: 'gcp_service_account', gcp_service_account: { key_json: '{"type":"service_account"}' } })
    expect(modal().getByText('cloudpam@staging-net.iam.gserviceaccount.com')).toBeTruthy()
    expect((modal().getByLabelText('Service account key JSON (required)') as HTMLTextAreaElement).value).toBe('')
    expect(showToast).toHaveBeenCalledWith('Credential saved', 'success')
  })

  it('omits an empty external id from the AWS body', async () => {
    serve({
      GET: () => json(404, { error: 'no credential stored for this account' }),
      PUT: () => json(200, { ...storedGCP, account_id: 8, type: 'aws_assume_role', summary: { role_arn: 'arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole' } }),
    })
    renderModal(awsAccount)
    await waitFor(() => expect(badge()).toBe('None stored'))

    fireEvent.change(modal().getByLabelText('Role ARN (required)'), { target: { value: ' arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole ' } })
    fireEvent.click(modal().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(badge()).toBe('Stored'))
    expect(calls.find(c => c.method === 'PUT')?.body).toEqual({
      type: 'aws_assume_role',
      aws_assume_role: { role_arn: 'arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole' },
    })
    expect(modal().getByText('not set')).toBeTruthy()
  })

  it('tells the operator to set the encryption key on a 409', async () => {
    const serverMessage = 'cannot store a credential: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key'
    serve({
      GET: () => json(404, { error: 'no credential stored for this account' }),
      PUT: () => json(409, { error: serverMessage }),
    })
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('None stored'))

    fireEvent.change(modal().getByLabelText('Service account key JSON (required)'), { target: { value: '{}' } })
    fireEvent.click(modal().getByRole('button', { name: 'Save' }))

    const alert = await screen.findByTestId('credential-save-error')
    expect(alert.textContent).toContain(serverMessage)
    expect(alert.textContent).toContain('Set CLOUDPAM_OIDC_ENCRYPTION_KEY to a 32-byte hex key on the server, restart it, then save again.')
    expect(badge()).toBe('None stored')
    expect((modal().getByLabelText('Service account key JSON (required)') as HTMLTextAreaElement).value).toBe('{}')
  })

  it('shows error and detail on a 400', async () => {
    serve({
      GET: () => json(404, { error: 'no credential stored for this account' }),
      PUT: () => json(400, { error: 'invalid gcp_service_account.key_json', detail: 'must include client_email' }),
    })
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('None stored'))

    fireEvent.change(modal().getByLabelText('Service account key JSON (required)'), { target: { value: '{"type":"service_account"}' } })
    fireEvent.click(modal().getByRole('button', { name: 'Save' }))

    const alert = await screen.findByTestId('credential-save-error')
    expect(alert.textContent).toContain('invalid gcp_service_account.key_json')
    expect(alert.textContent).toContain('must include client_email')
    expect(alert.textContent).not.toContain('restart')
  })

  it('renders a failed test result with the sanitized error and no identity', async () => {
    const result: CredentialTestResult = { tested_at: '2026-01-03T00:00:00Z', status: 'failed', error: 'get_project: http 403' }
    const tested: AccountCredential = { ...storedGCP, last_test: { tested_at: result.tested_at, status: 'failed', error: result.error } }
    let loads = 0
    serve({
      GET: () => json(200, loads++ === 0 ? storedGCP : tested),
      POST: () => json(200, result),
    })
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('Stored'))

    fireEvent.click(modal().getByRole('button', { name: 'Test' }))

    const shown = await screen.findByTestId('credential-test-result')
    expect(shown.textContent).toContain('failed')
    expect(shown.textContent).toContain('get_project: http 403')
    expect(calls.find(c => c.method === 'POST')?.url).toBe('/api/v1/accounts/7/credentials/test')
    await waitFor(() => expect(badge()).toBe('Failing'))
  })

  it('renders a passing test result with the identity', async () => {
    const result: CredentialTestResult = { tested_at: '2026-01-03T00:00:00Z', status: 'ok', identity: 'cloudpam@staging-net.iam.gserviceaccount.com' }
    serve({
      GET: () => json(200, storedGCP),
      POST: () => json(200, result),
    })
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('Stored'))

    fireEvent.click(modal().getByRole('button', { name: 'Test' }))

    const shown = await screen.findByTestId('credential-test-result')
    expect(shown.textContent).toContain('ok')
    expect(within(shown).getByText('cloudpam@staging-net.iam.gserviceaccount.com')).toBeTruthy()
  })

  it('clears after confirm and returns to the none state', async () => {
    serve({
      GET: () => json(200, storedGCP),
      DELETE: () => new Response(null, { status: 204 }),
    })
    vi.stubGlobal('confirm', vi.fn(() => true))
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('Stored'))

    fireEvent.click(modal().getByRole('button', { name: 'Clear' }))

    await waitFor(() => expect(badge()).toBe('None stored'))
    expect(confirm).toHaveBeenCalledWith('Clear the stored credential for "staging-net"?')
    expect(calls.find(c => c.method === 'DELETE')?.url).toBe('/api/v1/accounts/7/credentials')
    expect(showToast).toHaveBeenCalledWith('Credential cleared', 'success')
  })

  it('does nothing when the clear confirm is declined', async () => {
    serve({ GET: () => json(200, storedGCP) })
    vi.stubGlobal('confirm', vi.fn(() => false))
    renderModal(gcpAccount)
    await waitFor(() => expect(badge()).toBe('Stored'))

    fireEvent.click(modal().getByRole('button', { name: 'Clear' }))

    expect(calls.filter(c => c.method === 'DELETE')).toHaveLength(0)
    expect(badge()).toBe('Stored')
  })
})
