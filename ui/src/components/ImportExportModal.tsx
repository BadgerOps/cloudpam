import { useState, useRef } from 'react'
import { Download, Upload, X, FileText } from 'lucide-react'
import { useToast } from '../hooks/useToast'
import { post } from '../api/client'
import type { ImportResult } from '../api/types'

interface ImportExportModalProps {
  open: boolean
  onClose: () => void
}

export default function ImportExportModal({ open, onClose }: ImportExportModalProps) {
  const { showToast } = useToast()
  const [exportAccounts, setExportAccounts] = useState(true)
  const [exportPools, setExportPools] = useState(true)
  const [exportBlocks, setExportBlocks] = useState(true)
  const [exporting, setExporting] = useState(false)

  const [importType, setImportType] = useState<'accounts' | 'pools'>('accounts')
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importPreview, setImportPreview] = useState<string[][]>([])
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState<ImportResult | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  if (!open) return null

  async function handleExport() {
    const datasets: string[] = []
    if (exportAccounts) datasets.push('accounts')
    if (exportPools) datasets.push('pools')
    if (exportBlocks) datasets.push('blocks')
    if (datasets.length === 0) {
      showToast('Select at least one dataset', 'error')
      return
    }
    setExporting(true)
    try {
      const res = await fetch(`/api/v1/export?datasets=${datasets.join(',')}`)
      if (!res.ok) throw new Error('Export failed')
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `cloudpam-export-${new Date().toISOString().slice(0, 10)}.zip`
      a.click()
      URL.revokeObjectURL(url)
      showToast('Export downloaded', 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Export failed', 'error')
    } finally {
      setExporting(false)
    }
  }

  function handleFileSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setImportFile(file)
    setImportResult(null)

    // Parse CSV preview
    const reader = new FileReader()
    reader.onload = () => {
      const text = reader.result as string
      const lines = text.split('\n').filter(l => l.trim())
      const rows = lines.slice(0, 6).map(l => l.split(',').map(c => c.trim().replace(/^"|"$/g, '')))
      setImportPreview(rows)
    }
    reader.readAsText(file)
  }

  async function handleImport() {
    if (!importFile) return
    setImporting(true)
    setImportResult(null)
    try {
      const text = await importFile.text()
      const result = await post<ImportResult>(`/api/v1/import/${importType}`, text)
      setImportResult(result)
      showToast(`Imported ${result.created} ${importType}`, 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Import failed', 'error')
    } finally {
      setImporting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onClose}>
      <div
        className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-4xl max-h-[85vh] overflow-auto"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Import / Export</h2>
          <button onClick={onClose} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-700 rounded">
            <X className="w-5 h-5 text-gray-500 dark:text-gray-400" />
          </button>
        </div>

        <div className="grid grid-cols-2 divide-x dark:divide-gray-700">
          {/* Export */}
          <div className="p-6">
            <h3 className="flex items-center gap-2 font-semibold text-gray-900 dark:text-gray-100 mb-4">
              <Download className="w-4 h-4" />
              Export Data
            </h3>
            <div className="space-y-2 mb-4">
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={exportAccounts} onChange={e => setExportAccounts(e.target.checked)} className="rounded" />
                Accounts
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={exportPools} onChange={e => setExportPools(e.target.checked)} className="rounded" />
                Pools
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={exportBlocks} onChange={e => setExportBlocks(e.target.checked)} className="rounded" />
                Blocks
              </label>
            </div>
            <button
              onClick={handleExport}
              disabled={exporting}
              className="w-full px-4 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting ? 'Exporting...' : 'Download ZIP'}
            </button>
          </div>

          {/* Import */}
          <div className="p-6">
            <h3 className="flex items-center gap-2 font-semibold text-gray-900 dark:text-gray-100 mb-4">
              <Upload className="w-4 h-4" />
              Import Data
            </h3>
            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Import Type</label>
                <select
                  value={importType}
                  onChange={e => setImportType(e.target.value as 'accounts' | 'pools')}
                  className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                >
                  <option value="accounts">Accounts</option>
                  <option value="pools">Pools</option>
                </select>
              </div>

              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">CSV File</label>
                <input
                  ref={fileRef}
                  type="file"
                  accept=".csv"
                  onChange={handleFileSelect}
                  className="hidden"
                />
                <button
                  onClick={() => fileRef.current?.click()}
                  className="w-full flex items-center justify-center gap-2 px-4 py-3 border-2 border-dashed dark:border-gray-600 rounded-lg text-sm text-gray-500 dark:text-gray-400 hover:border-blue-300 dark:hover:border-blue-700 hover:text-blue-600 dark:hover:text-blue-400"
                >
                  <FileText className="w-4 h-4" />
                  {importFile ? importFile.name : 'Choose CSV file...'}
                </button>
              </div>

              {/* Preview */}
              {importPreview.length > 0 && (
                <div className="overflow-x-auto">
                  <table className="min-w-full text-xs border dark:border-gray-600 rounded">
                    <thead>
                      <tr className="bg-gray-50 dark:bg-gray-800">
                        {importPreview[0].map((h, i) => (
                          <th key={i} className="px-2 py-1 text-left font-medium text-gray-500 dark:text-gray-400">{h}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {importPreview.slice(1, 6).map((row, ri) => (
                        <tr key={ri} className="border-t dark:border-gray-700">
                          {row.map((cell, ci) => (
                            <td key={ci} className="px-2 py-1 text-gray-600 dark:text-gray-300 truncate max-w-[120px]">{cell}</td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              <button
                onClick={handleImport}
                disabled={importing || !importFile}
                className="w-full px-4 py-2 bg-green-600 text-white text-sm rounded hover:bg-green-700 disabled:opacity-50"
              >
                {importing ? 'Importing...' : 'Import'}
              </button>

              {/* Result */}
              {importResult && (
                <div className="bg-gray-50 dark:bg-gray-800 border dark:border-gray-600 rounded p-3 text-sm">
                  <div className="text-green-700 dark:text-green-300">Created: {importResult.created}</div>
                  <div className="text-gray-500 dark:text-gray-400">Skipped: {importResult.skipped}</div>
                  {importResult.errors.length > 0 && (
                    <div className="text-red-600 dark:text-red-400 mt-1">
                      Errors: {importResult.errors.join('; ')}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* CSV format help */}
        <div className="px-6 py-4 bg-gray-50 dark:bg-gray-800 border-t dark:border-gray-700 text-xs text-gray-500 dark:text-gray-400">
          <strong>CSV Format:</strong> Accounts: key,name,provider,tier,environment,regions |
          Pools: name,cidr,parent_id,account_id,type,status,description
        </div>
      </div>
    </div>
  )
}
