export default function FormError({ message }: { message: string | null }) {
  if (!message) return null
  return (
    <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded" role="alert">
      {message}
    </div>
  )
}
