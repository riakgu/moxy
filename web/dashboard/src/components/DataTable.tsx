import { useState, useMemo } from 'react'

export interface Column<T> {
  key: string
  label: string
  sortable?: boolean
  render?: (row: T) => React.ReactNode
}

interface DataTableProps<T> {
  columns: Column<T>[]
  data: T[]
  pageSize?: number
  keyField: string
}

type SortDir = 'asc' | 'desc'

export default function DataTable<T extends Record<string, unknown>>({
  columns,
  data,
  pageSize = 20,
  keyField,
}: DataTableProps<T>) {
  const [sortKey, setSortKey] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [page, setPage] = useState(1)
  const [itemsPerPage, setItemsPerPage] = useState(pageSize)

  const sorted = useMemo(() => {
    if (!sortKey) return data
    return [...data].sort((a, b) => {
      const av = a[sortKey]
      const bv = b[sortKey]
      if (av == null || bv == null) return 0
      const cmp = av < bv ? -1 : av > bv ? 1 : 0
      return sortDir === 'asc' ? cmp : -cmp
    })
  }, [data, sortKey, sortDir])

  const totalPages = Math.max(1, Math.ceil(sorted.length / itemsPerPage))
  const paged = sorted.slice((page - 1) * itemsPerPage, page * itemsPerPage)

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir(d => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
    setPage(1)
  }

  const sortIcon = (key: string) => {
    if (sortKey !== key) return '↕'
    return sortDir === 'asc' ? '↑' : '↓'
  }

  return (
    <div className="bg-bg-card rounded-xl border border-border overflow-hidden">
      <div className="overflow-x-auto">
        <table className="min-w-full text-sm divide-y divide-border">
          <thead className="bg-bg-hover">
            <tr>
              {columns.map(col => (
                <th
                  key={col.key}
                  onClick={() => col.sortable && handleSort(col.key)}
                  className={`px-4 py-3 text-left font-semibold text-text-secondary ${
                    col.sortable ? 'cursor-pointer hover:bg-border select-none' : ''
                  }`}
                >
                  {col.label} {col.sortable && <span className="text-text-muted ml-1">{sortIcon(col.key)}</span>}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {paged.map(row => (
              <tr key={String(row[keyField])} className="hover:bg-bg-hover/50 transition-colors">
                {columns.map(col => (
                  <td key={col.key} className="px-4 py-3 text-text-primary">
                    {col.render ? col.render(row) : String(row[col.key] ?? '—')}
                  </td>
                ))}
              </tr>
            ))}
            {paged.length === 0 && (
              <tr>
                <td colSpan={columns.length} className="px-4 py-12 text-center text-text-muted">
                  No data
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {sorted.length > 0 && (
        <div className="flex items-center justify-between px-4 py-3 border-t border-border">
          <div className="flex items-center gap-3">
            <span className="text-sm text-text-secondary">
              {Math.min((page - 1) * itemsPerPage + 1, sorted.length)}–{Math.min(page * itemsPerPage, sorted.length)} of {sorted.length}
            </span>
            <select
              value={itemsPerPage}
              onChange={e => { setItemsPerPage(Number(e.target.value)); setPage(1) }}
              className="text-sm bg-bg-input border border-border rounded-lg px-2 py-1 text-text-primary"
            >
              <option value={20}>20</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
            </select>
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => setPage(p => p - 1)}
              disabled={page <= 1}
              className="px-3 py-1 rounded-lg text-sm font-medium bg-bg-hover text-text-secondary hover:bg-border disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Prev
            </button>
            <span className="px-3 py-1 text-sm text-text-secondary">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage(p => p + 1)}
              disabled={page >= totalPages}
              className="px-3 py-1 rounded-lg text-sm font-medium bg-bg-hover text-text-secondary hover:bg-border disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
