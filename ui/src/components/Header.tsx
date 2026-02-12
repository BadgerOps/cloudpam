import { Search, Bell, User } from 'lucide-react'

interface HeaderProps {
  onSearchClick: () => void
}

export default function Header({ onSearchClick }: HeaderProps) {
  return (
    <header className="h-16 bg-white border-b border-gray-200 flex items-center justify-between px-6 flex-shrink-0">
      <div className="flex items-center gap-4 flex-1">
        <div className="relative w-96">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400" />
          <input
            type="text"
            readOnly
            onClick={onSearchClick}
            onFocus={onSearchClick}
            placeholder="Search pools, CIDRs, accounts..."
            className="w-full pl-10 pr-16 py-2 border border-gray-300 rounded-lg hover:border-gray-400 cursor-pointer outline-none"
          />
          <kbd className="absolute right-3 top-1/2 -translate-y-1/2 px-2 py-0.5 bg-gray-100 text-gray-500 text-xs rounded">
            ⌘K
          </kbd>
        </div>
      </div>
      <div className="flex items-center gap-4">
        <button className="relative p-2 text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded-lg">
          <Bell className="w-5 h-5" />
        </button>
        <div className="flex items-center gap-2 pl-4 border-l border-gray-200">
          <div className="w-8 h-8 bg-blue-100 text-blue-600 rounded-full flex items-center justify-center">
            <User className="w-4 h-4" />
          </div>
          <span className="text-sm font-medium text-gray-700">Admin</span>
        </div>
      </div>
    </header>
  )
}
