import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { NetworkRelationshipTable } from '../pages/DiscoveryPage'
import type { NetworkRelationship } from '../api/types'

const relationship: NetworkRelationship = {
  id: 'contains/discovered/id/with/slashes',
  type: 'contains',
  source_kind: 'network_object',
  source_id: '1',
  target_kind: 'discovered',
  target_id: 'id/with/slashes',
  confidence: 1,
  reason: 'placeholder parent',
  evidence: ['parent_resource_id=vpc-missing'],
  resolution_state: 'open',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

describe('NetworkRelationshipTable', () => {
  it('requires Apply before submitting relationship resolution changes', async () => {
    const onResolve = vi.fn().mockResolvedValue(undefined)
    render(<NetworkRelationshipTable relationships={[relationship]} loading={false} onResolve={onResolve} />)

    fireEvent.change(screen.getByLabelText(`Resolution for ${relationship.id}`), { target: { value: 'accepted' } })
    fireEvent.change(screen.getByPlaceholderText('Reason'), { target: { value: 'reviewed relationship' } })
    expect(onResolve).not.toHaveBeenCalled()

    fireEvent.click(screen.getByLabelText(`Apply relationship ${relationship.id}`))

    await waitFor(() => {
      expect(onResolve).toHaveBeenCalledWith(relationship, 'accepted', 'reviewed relationship')
    })
  })

  it('shows inline errors when relationship resolution fails', async () => {
    const onResolve = vi.fn().mockRejectedValue(new Error('server rejected relationship update'))
    render(<NetworkRelationshipTable relationships={[relationship]} loading={false} onResolve={onResolve} />)

    fireEvent.change(screen.getByLabelText(`Resolution for ${relationship.id}`), { target: { value: 'rejected' } })
    fireEvent.click(screen.getByLabelText(`Apply relationship ${relationship.id}`))

    await waitFor(() => {
      expect(screen.getByText('server rejected relationship update')).toBeTruthy()
    })
  })
})
