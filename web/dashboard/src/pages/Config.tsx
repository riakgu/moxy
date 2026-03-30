import { useEffect, useState } from 'react'
import { apiFetch } from '../api/client'
import type { MoxyConfig } from '../api/types'

export default function Config() {
  const [rawText, setRawText] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    // Note: GET /api/config is a placeholder for backend implementation
    apiFetch<Exclude<MoxyConfig, undefined>>('/config')
      .then(res => {
        setRawText(JSON.stringify(res, null, 2))
      })
      .catch(err => {
        console.error('Config fetch error. Assuming missing backend for now.', err)
        const mock: Partial<MoxyConfig> = { proxy: { socks5_port: 1080 } as any }
        setRawText(JSON.stringify(mock, null, 2))
      })
      .finally(() => setLoading(false))
  }, [])

  const handleSave = async () => {
    setSaving(true)
    try {
      const parsed = JSON.parse(rawText)
      await apiFetch('/config', { method: 'PUT', body: JSON.stringify(parsed) })
      alert('Config saved')
    } catch (e) {
      alert(`Invalid JSON or save error: ${e instanceof Error ? e.message : 'Unknown'}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="max-w-4xl">
      <h2 className="text-2xl font-bold mb-6">Runtime Configuration</h2>
      <p className="text-sm text-text-secondary mb-6">Edit the Moxy runtime configuration. Changes to some settings may require a server restart.</p>
      
      {loading ? (
        <div>Loading...</div>
      ) : (
        <div className="bg-bg-card border border-border rounded-xl p-4 overflow-hidden flex flex-col h-[600px]">
          <textarea
            value={rawText}
            onChange={e => setRawText(e.target.value)}
            className="flex-1 w-full bg-[#000] text-text-primary font-mono text-sm p-4 rounded-lg resize-none outline-none focus:ring-1 focus:ring-accent"
            spellCheck={false}
          />
          <div className="flex justify-end mt-4">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-6 py-2 bg-accent text-white font-medium rounded-lg hover:bg-accent-hover disabled:opacity-50 transition-colors"
            >
              {saving ? 'Saving...' : 'Save Configuration'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
