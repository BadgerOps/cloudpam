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
  // With no htmlFor the single child is the control, so the field wires its
  // ARIA attributes and reuses the child's own id when it has one. With
  // htmlFor the caller placed the id and keeps ownership.
  const child = htmlFor === undefined && isValidElement<ControlProps>(children)
    ? (children as ReactElement<ControlProps>)
    : null
  const controlId = htmlFor ?? child?.props.id ?? generatedId
  const hintId = `${controlId}-hint`
  const errorId = `${controlId}-error`
  const describedBy = error ? errorId : hint ? hintId : undefined

  const control = child
    ? cloneElement(child, {
        id: controlId,
        'aria-invalid': error ? true : child.props['aria-invalid'],
        'aria-describedby': describedBy ?? child.props['aria-describedby'],
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
