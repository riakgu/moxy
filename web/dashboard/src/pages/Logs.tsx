import { usePolling } from '../hooks/usePolling'
import { apiFetch } from '../api/client'
import type { LogsResponse } from '../api/types'

export default function Logs() {
  // Use /api/logs/tail?lines=100 - placeholder for backend implementation
  const { data, loading, error } = usePolling(
    () => apiFetch<LogsResponse>('/logs/tail?lines=200').catch(() => ({ 
      lines: ['[backend endpoint /api/logs/tail missing - mock data]', 'time="2026-03-30T10:00:00Z" level=info msg="Starting Moxy"'],
      file: 'moxy.log'
    })), 
    { intervalMs: 3000 }
  )

  const lines = data?.lines ?? []

  return (
    <div className="h-[calc(100vh-8rem)] flex flex-col">
      <div className="flex justify-between items-end mb-4 shrink-0">
        <div>
          <h2 className="text-2xl font-bold mb-1">System Logs</h2>
          <p className="text-sm text-text-secondary">Tailing active log file: <span className="font-mono">{data?.file || '...'}</span></p>
        </div>
      </div>

      <div className="flex-1 bg-[#090b10] border border-border rounded-xl p-4 overflow-y-auto font-mono text-xs text-text-secondary shadow-inner">
        {loading && lines.length === 0 ? (
          <div>Loading logs...</div>
        ) : error ? (
          <div className="text-danger flex h-full items-center justify-center">Error fetching logs: {error}</div>
        ) : (
          <div className="space-y-1">
            {lines.map((line, i) => {
              // Simple syntax highlighting heuristic
              const isError = line.toLowerCase().includes('level=error') || line.toLowerCase().includes('level=fatal')
              const isInfo = line.toLowerCase().includes('level=info')
              const isWarn = line.toLowerCase().includes('level=warn')
              
              const colorCls = isError ? 'text-danger' : isWarn ? 'text-warning' : isInfo ? 'text-text-primary' : 'text-text-muted'
              
              return (
                <div key={i} className={`${colorCls} break-all hover:bg-white/5 px-1 rounded`}>
                  {line}
                </div>
              )
            })}
            {lines.length === 0 && <div className="text-center mt-10">No logs available</div>}
          </div>
        )}
      </div>
    </div>
  )
}
