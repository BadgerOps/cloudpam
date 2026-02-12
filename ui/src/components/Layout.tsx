import { useState, useCallback, useEffect } from 'react'
import { Outlet } from 'react-router-dom'
import Sidebar from './Sidebar'
import Header from './Header'
import SearchModal from './SearchModal'
import ImportExportModal from './ImportExportModal'
import ToastContainer from './ToastContainer'

export default function Layout() {
  const [searchOpen, setSearchOpen] = useState(false)
  const [importExportOpen, setImportExportOpen] = useState(false)

  const openSearch = useCallback(() => setSearchOpen(true), [])

  useEffect(() => {
    const handleKeydown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [])

  return (
    <div className="h-screen flex">
      <Sidebar onImportExport={() => setImportExportOpen(true)} />
      <div className="flex-1 flex flex-col overflow-hidden">
        <Header onSearchClick={openSearch} />
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
      <SearchModal open={searchOpen} onClose={() => setSearchOpen(false)} />
      <ImportExportModal open={importExportOpen} onClose={() => setImportExportOpen(false)} />
      <ToastContainer />
    </div>
  )
}
