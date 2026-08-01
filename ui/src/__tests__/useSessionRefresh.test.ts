import { describe, expect, it } from 'vitest'
import { parseRefreshMessage } from '../hooks/useSessionRefresh'

const ORIGIN = 'https://cloudpam.example.com'

// Stand-in for the hidden refresh iframe's contentWindow.
const iframeWindow = {} as MessageEventSource
const otherWindow = {} as MessageEventSource

function event(overrides: Partial<MessageEvent> = {}): MessageEvent {
  return {
    origin: ORIGIN,
    source: iframeWindow,
    data: { type: 'oidc-refresh', success: true },
    ...overrides,
  } as MessageEvent
}

describe('parseRefreshMessage', () => {
  it('accepts a well-formed same-origin message from the refresh iframe', () => {
    expect(parseRefreshMessage(event(), ORIGIN, iframeWindow)).toEqual({
      type: 'oidc-refresh',
      success: true,
    })
  })

  it('accepts a failure report from the refresh iframe', () => {
    const msg = event({ data: { type: 'oidc-refresh', success: false } })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toEqual({
      type: 'oidc-refresh',
      success: false,
    })
  })

  it('rejects a message from a different origin', () => {
    const msg = event({ origin: 'https://attacker.example.com' })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toBeNull()
  })

  it('rejects an origin that merely shares a prefix with ours', () => {
    const msg = event({ origin: `${ORIGIN}.attacker.example.com` })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toBeNull()
  })

  it('rejects a same-origin message from a window other than the refresh iframe', () => {
    const msg = event({ source: otherWindow })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toBeNull()
  })

  it('rejects any message when no refresh is in flight', () => {
    expect(parseRefreshMessage(event(), ORIGIN, null)).toBeNull()
    expect(parseRefreshMessage(event(), ORIGIN, undefined)).toBeNull()
  })

  it('rejects messages with the wrong type', () => {
    const msg = event({ data: { type: 'other', success: true } })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toBeNull()
  })

  it('rejects messages whose success flag is not a boolean', () => {
    const msg = event({ data: { type: 'oidc-refresh', success: 'yes' } })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toBeNull()
  })

  it('rejects non-object payloads', () => {
    expect(parseRefreshMessage(event({ data: null }), ORIGIN, iframeWindow)).toBeNull()
    expect(parseRefreshMessage(event({ data: 'oidc-refresh' }), ORIGIN, iframeWindow)).toBeNull()
    expect(parseRefreshMessage(event({ data: undefined }), ORIGIN, iframeWindow)).toBeNull()
  })

  it('returns a normalised message rather than the attacker-controlled object', () => {
    const msg = event({
      data: { type: 'oidc-refresh', success: true, extra: 'ignored' },
    })
    expect(parseRefreshMessage(msg, ORIGIN, iframeWindow)).toEqual({
      type: 'oidc-refresh',
      success: true,
    })
  })
})
