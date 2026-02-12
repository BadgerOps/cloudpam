import { Search, Bell, User } from 'lucide-react'

interface HeaderProps {
  onSearchClick: () => void
}

export default function Header({ onSearchClick }: HeaderProps) {
  return (
    <header className="h-16 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between px-6 flex-shrink-0">
      <div className="flex items-center gap-4 flex-1">
        <div className="relative w-96">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 dark:text-gray-500" />
          <input
            type="text"
            readOnly
            onClick={onSearchClick}
            onFocus={onSearchClick}
            placeholder="Search pools, CIDRs, accounts..."
            className="w-full pl-10 pr-16 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:border-gray-400 dark:hover:border-gray-500 cursor-pointer outline-none dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-400"
          />
          <kbd className="absolute right-3 top-1/2 -translate-y-1/2 px-2 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 text-xs rounded">
            ⌘K
          </kbd>
        </div>
      </div>
      <div className="flex items-center gap-4">
        <button className="relative p-2 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg">
          <Bell className="w-5 h-5" />
        </button>
        <div className="flex items-center gap-2 pl-4 border-l border-gray-200 dark:border-gray-700">
          <div className="w-8 h-8 bg-blue-100 dark:bg-blue-900 text-blue-600 dark:text-blue-400 rounded-full flex items-center justify-center">
            <User className="w-4 h-4" />
          </div>
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Admin</span>
        </div>
      </div>
    </header>
  )
}
