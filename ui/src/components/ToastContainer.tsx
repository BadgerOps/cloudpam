import { useToast } from '../hooks/useToast'

export default function ToastContainer() {
  const { toasts } = useToast()

  if (toasts.length === 0) return null

  return (
    <div className="fixed right-4 bottom-4 flex flex-col gap-2 z-50">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`bg-white border rounded-lg shadow-lg px-4 py-3 min-w-60 ${
            t.type === 'error' ? 'border-red-200' : t.type === 'success' ? 'border-green-200' : 'border-blue-200'
          }`}
        >
          <div className="font-medium text-sm">{t.type === 'error' ? 'Error' : t.type === 'success' ? 'Success' : 'Info'}</div>
          <div className="text-sm text-gray-600">{t.message}</div>
        </div>
      ))}
    </div>
  )
}
