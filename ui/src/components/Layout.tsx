import { useState, useCallback, useEffect } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import Sidebar from './Sidebar'
import Header from './Header'
import SearchModal from './SearchModal'
import ImportExportModal from './ImportExportModal'
import ToastContainer from './ToastContainer'
import UpdateBanner from './UpdateBanner'
import { useSessionRefresh } from '../hooks/useSessionRefresh'

export default function Layout() {
  useSessionRefresh()
  const [searchOpen, setSearchOpen] = useState(false)
  const [importExportOpen, setImportExportOpen] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { pathname } = useLocation()

  const openSearch = useCallback(() => setSearchOpen(true), [])
  const closeSidebar = useCallback(() => setSidebarOpen(false), [])
  const toggleSidebar = useCallback(() => setSidebarOpen(v => !v), [])

  // Close the mobile drawer whenever navigation happens.
  useEffect(() => {
    setSidebarOpen(false)
  }, [pathname])

  useEffect(() => {
    const handleKeydown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setSearchOpen(true)
      }
      if (e.key === 'Escape') {
        setSidebarOpen(false)
      }
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [])

  return (
    <div className="h-screen flex bg-gray-100 dark:bg-gray-900">
      <Sidebar
        onImportExport={() => setImportExportOpen(true)}
        open={sidebarOpen}
        onClose={closeSidebar}
      />
      {sidebarOpen && (
        <div
          data-testid="sidebar-backdrop"
          aria-hidden="true"
          onClick={closeSidebar}
          className="fixed inset-0 z-30 bg-black/50 md:hidden"
        />
      )}
      <div className="flex-1 flex flex-col overflow-hidden min-w-0">
        <Header onSearchClick={openSearch} onMenuClick={toggleSidebar} sidebarOpen={sidebarOpen} />
        <UpdateBanner />
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
