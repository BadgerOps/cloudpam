import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiRequestError } from '../api/client'
import type { Pool } from '../api/types'
import { cascadePoolCount, deleteWithCascade, plural } from '../utils/cascadeDelete'

const conflict = () => new ApiRequestError(409, { error: 'pool has child pools: conflict' })

describe('deleteWithCascade', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('deletes without a prompt when the plain delete succeeds', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const del = vi.fn().mockResolvedValue(undefined)
    const describe = vi.fn()

    await expect(deleteWithCascade(del, describe)).resolves.toBe(true)

    expect(del.mock.calls).toEqual([[false]])
    expect(describe).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('asks with the cascade description and resends with force on a 409', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const del = vi.fn().mockRejectedValueOnce(conflict()).mockResolvedValueOnce(undefined)

    await expect(deleteWithCascade(del, async () => 'Delete with 3 children?')).resolves.toBe(true)

    expect(confirm).toHaveBeenCalledWith('Delete with 3 children?')
    expect(del.mock.calls).toEqual([[false], [true]])
  })

  it('leaves the target alone when the user declines the cascade', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const del = vi.fn().mockRejectedValueOnce(conflict())

    await expect(deleteWithCascade(del, async () => 'Delete with 3 children?')).resolves.toBe(false)

    expect(del.mock.calls).toEqual([[false]])
  })

  it('propagates any other failure without prompting', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const forbidden = new ApiRequestError(403, { error: 'forbidden' })
    const del = vi.fn().mockRejectedValueOnce(forbidden).mockRejectedValueOnce(new Error('network down'))

    await expect(deleteWithCascade(del, async () => 'unused')).rejects.toBe(forbidden)
    await expect(deleteWithCascade(del, async () => 'unused')).rejects.toThrow('network down')

    expect(confirm).not.toHaveBeenCalled()
    expect(del.mock.calls).toEqual([[false], [false]])
  })
})

describe('cascadePoolCount', () => {
  function pool(id: number, parent_id: number | null, account_id: number | null): Pool {
    return {
      id, parent_id, account_id, name: `p${id}`, cidr: `10.${id}.0.0/16`,
      type: 'vpc', status: 'active', source: 'manual',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }
  }

  const pools = [
    pool(1, null, 1),
    pool(2, 1, null),
    pool(3, 2, 2),
    pool(4, null, 2),
    pool(5, null, null),
  ]

  it('counts the assigned pools and everything under them once', () => {
    expect(cascadePoolCount(pools, 1)).toBe(3)
    expect(cascadePoolCount(pools, 2)).toBe(2)
  })

  it('is zero for an account with no pools', () => {
    expect(cascadePoolCount(pools, 9)).toBe(0)
    expect(cascadePoolCount([], 1)).toBe(0)
  })
})

describe('plural', () => {
  it('picks the singular only for exactly one', () => {
    expect(plural(1, 'pool')).toBe('1 pool')
    expect(plural(0, 'pool')).toBe('0 pools')
    expect(plural(2, 'child', 'children')).toBe('2 children')
  })
})
