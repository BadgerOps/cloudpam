import { cloneElement, isValidElement, useId } from 'react'
import type { ReactElement, ReactNode } from 'react'

export interface FieldProps {
  label: string
  hint?: string
  error?: string
  required?: boolean
  htmlFor?: string
  children: ReactNode
}

type ControlProps = {
  id?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}

export function Field({ label, hint, error, required, htmlFor, children }: FieldProps) {
  const generatedId = useId()
  const controlId = htmlFor ?? generatedId
  const hintId = `${controlId}-hint`
  const errorId = `${controlId}-error`
  const describedBy = error ? errorId : hint ? hintId : undefined

  // With no htmlFor the single child is the control, so the field owns its id
  // and ARIA wiring. With htmlFor the caller placed the id and keeps ownership.
  const control = htmlFor === undefined && isValidElement<ControlProps>(children)
    ? cloneElement(children as ReactElement<ControlProps>, {
        id: children.props.id ?? controlId,
        'aria-invalid': error ? true : children.props['aria-invalid'],
        'aria-describedby': describedBy ?? children.props['aria-describedby'],
      })
    : children

  return (
    <div>
      <label htmlFor={controlId} className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
        {label}
        {required && <span aria-hidden="true" className="ml-0.5 text-red-500">*</span>}
      </label>
      {control}
      {error ? (
        <p id={errorId} role="alert" className="mt-1 text-xs text-red-600 dark:text-red-400">{error}</p>
      ) : hint ? (
        <p id={hintId} className="mt-1 text-xs text-gray-500 dark:text-gray-400">{hint}</p>
      ) : null}
    </div>
  )
}
