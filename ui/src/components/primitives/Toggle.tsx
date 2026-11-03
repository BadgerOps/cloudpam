export interface ToggleProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  hint?: string
  disabled?: boolean
}

export function Toggle({ checked, onChange, label, hint, disabled }: ToggleProps) {
  return (
    <label className={`flex items-start gap-3 ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={`${label}, ${checked ? 'on' : 'off'}`}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`relative mt-0.5 inline-flex h-5 w-9 flex-shrink-0 rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 dark:focus:ring-offset-gray-800 ${
          checked ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'
        }`}
      >
        <span
          className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0'
          }`}
        />
      </button>
      <span className="min-w-0">
        <span className="block text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
        {hint && <span className="block text-xs text-gray-500 dark:text-gray-400">{hint}</span>}
      </span>
    </label>
  )
}
