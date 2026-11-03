export interface EmptyStateProps {
  title: string
  description?: string
  action?: { label: string; onClick: () => void }
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center text-center px-6 py-12">
      <p className="text-sm font-medium text-gray-900 dark:text-white">{title}</p>
      {description && <p className="mt-1 text-sm text-gray-500 dark:text-gray-400 max-w-md">{description}</p>}
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="mt-4 px-3 py-1.5 rounded-lg text-sm font-medium bg-blue-600 text-white hover:bg-blue-700"
        >
          {action.label}
        </button>
      )}
    </div>
  )
}
