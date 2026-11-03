import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ConnectivityBadge from '../components/ConnectivityBadge'
import { resetAccountConnectivity } from '../hooks/useAccountConnectivity'
import { ApiRequestError } from '../api/client'
import type { ConnectivityTestResult } from '../api/types'

const mockPost = vi.hoisted(() => vi.fn())

vi.mock('../api/client', async importOriginal => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  post: mockPost,
}))

const ok: ConnectivityTestResult = {
  tested_at: new Date().toISOString(),
  status: 'ok',
  latency_ms: 42,
  identity: 'arn:aws:sts::111111111111:assumed-role/CloudPAM/cloudpam-1',
  source: 'stored_credential',
}

beforeEach(() => {
  vi.clearAllMocks()
  resetAccountConnectivity()
})

describe('ConnectivityBadge', () => {
  it('tests the account and shows the result wherever that account is shown', async () => {
    mockPost.mockResolvedValue(ok)
    render(
      <>
        <ConnectivityBadge accountId={1} accountName="prod" />
        <ConnectivityBadge accountId={1} accountName="prod" showDetail />
        <ConnectivityBadge accountId={2} accountName="staging" />
      </>,
    )

    fireEvent.click(screen.getAllByRole('button', { name: 'Test connectivity for prod' })[0])

    await waitFor(() => expect(screen.getAllByText('connected · 42 ms')).toHaveLength(2))
    expect(mockPost).toHaveBeenCalledTimes(1)
    expect(mockPost).toHaveBeenCalledWith('/api/v1/accounts/1/test', {})
    expect(screen.getByRole('status').textContent).toContain('Reached as arn:aws:sts::111111111111:assumed-role/CloudPAM/cloudpam-1 (stored credential, 42 ms')
    expect(screen.getByRole('button', { name: 'Test connectivity for staging' }).textContent).toBe('Test')
  })

  it('shows a failed probe with its sanitized error', async () => {
    mockPost.mockResolvedValue({ ...ok, status: 'failed', identity: undefined, error: 'assume_role: AccessDenied (http 403)', source: 'host' })
    render(<ConnectivityBadge accountId={1} accountName="prod" showDetail />)

    fireEvent.click(screen.getByRole('button', { name: 'Test connectivity for prod' }))

    expect(await screen.findByText('failed')).toBeTruthy()
    expect(screen.getByRole('status').textContent).toContain('Failed: assume_role: AccessDenied (http 403) (server identity')
  })

  it('says the account was not tested when the server refuses the test', async () => {
    mockPost.mockRejectedValue(new ApiRequestError(422, { error: 'no connectivity test for this account provider: "azure"' }))
    render(<ConnectivityBadge accountId={1} accountName="prod" showDetail />)

    fireEvent.click(screen.getByRole('button', { name: 'Test connectivity for prod' }))

    expect(await screen.findByText('not tested')).toBeTruthy()
    expect(screen.getByRole('status').textContent).toBe('no connectivity test for this account provider: "azure"')
  })
})
