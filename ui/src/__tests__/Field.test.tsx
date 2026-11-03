import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Field } from '../components/primitives'

describe('Field', () => {
  it('associates the label with the control without a caller-supplied id', () => {
    render(
      <Field label="Session Duration (hours)" hint="How long a session cookie stays valid">
        <input type="number" />
      </Field>,
    )

    const input = screen.getByLabelText('Session Duration (hours)') as HTMLInputElement
    expect(input.type).toBe('number')
    expect(input.getAttribute('aria-invalid')).toBeNull()
    expect(screen.getByText('How long a session cookie stays valid').id).toBe(input.getAttribute('aria-describedby'))
  })

  it('marks an errored control invalid and announces the error in place of the hint', () => {
    render(
      <Field label="Minimum Length" hint="At least 8" error="Must be between 8 and 128">
        <input type="number" />
      </Field>,
    )

    const input = screen.getByLabelText('Minimum Length')
    expect(input.getAttribute('aria-invalid')).toBe('true')
    const error = screen.getByRole('alert')
    expect(error.textContent).toBe('Must be between 8 and 128')
    expect(input.getAttribute('aria-describedby')).toBe(error.id)
    expect(screen.queryByText('At least 8')).toBeNull()
  })

  it('respects a caller-supplied htmlFor and leaves the child untouched', () => {
    render(
      <Field label="Trusted Proxy CIDRs" htmlFor="proxies">
        <div>
          <textarea id="proxies" />
        </div>
      </Field>,
    )

    expect((screen.getByLabelText('Trusted Proxy CIDRs') as HTMLTextAreaElement).tagName).toBe('TEXTAREA')
  })

  it('shows a required marker that is hidden from assistive tech', () => {
    render(
      <Field label="Name" required>
        <input />
      </Field>,
    )

    expect(screen.getByRole('textbox', { name: 'Name' })).toBeTruthy()
    expect(screen.getByText('*').getAttribute('aria-hidden')).toBe('true')
  })
})
