import { useToast } from '../hooks/useToast'

export default function ToastContainer() {
  const { toasts } = useToast()

  if (toasts.length === 0) return null

  return (
    <div className="fixed right-4 bottom-4 flex flex-col gap-2 z-50">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`bg-white dark:bg-gray-800 border rounded-lg shadow-lg px-4 py-3 min-w-60 ${
            t.type === 'error' ? 'border-red-200 dark:border-red-800' : t.type === 'success' ? 'border-green-200 dark:border-green-800' : 'border-blue-200 dark:border-blue-800'
          }`}
        >
          <div className="font-medium text-sm">{t.type === 'error' ? 'Error' : t.type === 'success' ? 'Success' : 'Info'}</div>
          <div className="text-sm text-gray-600 dark:text-gray-300">{t.message}</div>
        </div>
      ))}
    </div>
  )
}
