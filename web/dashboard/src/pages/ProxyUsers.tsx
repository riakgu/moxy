import { useState, useMemo } from 'react'
import DataTable, { type Column } from '../components/DataTable'
import Modal from '../components/Modal'
import CopyButton from '../components/CopyButton'
import { usePolling } from '../hooks/usePolling'
import { proxyUsersApi } from '../api/proxyUsers'
import type { ProxyUser, CreateProxyUserRequest } from '../api/types'

export default function ProxyUsers() {
  const { data: users, refresh } = usePolling(() => proxyUsersApi.list(), { intervalMs: 5000 })
  const [openUserModal, setOpenUserModal] = useState(false)
  const [openGenModal, setOpenGenModal] = useState(false)
  
  // Create User form state
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [binding, setBinding] = useState('')

  // Gen string form state
  const [genCount, setGenCount] = useState(1)
  const [genAuthType, setGenAuthType] = useState('userpass')
  const [genPort, setGenPort] = useState(10000)
  const [genIps, setGenIps] = useState('')

  const handleCreateUser = async () => {
    try {
      const req: CreateProxyUserRequest = { username, password, device_binding: binding || undefined }
      await proxyUsersApi.create(req)
      setOpenUserModal(false)
      refresh()
    } catch (err) {
      alert(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const handleDelete = async (u: string) => {
    try {
      await proxyUsersApi.delete(u)
      refresh()
    } catch (err) {
      alert(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const handleToggle = async (u: ProxyUser) => {
    try {
      await proxyUsersApi.update(u.username, { enabled: !u.enabled })
      refresh()
    } catch (err) {
      alert(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const generatedStrings = useMemo(() => {
    if (!genIps) return []
    const ipList = genIps.split('\n').map(s => s.trim()).filter(Boolean)
    const results: string[] = []
    
    for (const ip of ipList) {
      if (genAuthType === 'userpass' && users && users.length > 0) {
        // use randomly selected users
        for (let i = 0; i < genCount; i++) {
          const u = users[i % users.length]
          results.push(`${ip}:1080:${u.username}:<password>`)
        }
      } else if (genAuthType === 'port') {
        const end = genPort + genCount
        for (let p = genPort; p < end; p++) {
          results.push(`${ip}:${p}`)
        }
      }
    }
    return results
  }, [genIps, genCount, genAuthType, genPort, users])

  const columns: Column<ProxyUser>[] = [
    { key: 'username', label: 'Username', sortable: true, render: r => <span className="font-semibold">{r.username}</span> },
    { key: 'device_binding', label: 'Binding', render: r => <span className="font-mono text-xs text-text-muted">{r.device_binding || 'None'}</span> },
    { key: 'enabled', label: 'Status', sortable: true, render: r => (
      <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${r.enabled ? 'bg-success-muted text-success' : 'bg-danger-muted text-danger'}`}>
        {r.enabled ? 'Enabled' : 'Disabled'}
      </span>
    )},
    { key: 'actions', label: 'Actions', render: r => (
      <div className="flex gap-2">
        <button onClick={() => handleToggle(r)} className="text-xs px-2 py-1 bg-bg-hover rounded hover:bg-border transition-colors">
          Toggle
        </button>
        <button onClick={() => handleDelete(r.username)} className="text-xs px-2 py-1 bg-danger-muted text-danger rounded hover:bg-danger hover:text-white transition-colors">
          Delete
        </button>
      </div>
    )}
  ]

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-2xl font-bold">Proxy Users & Generation</h2>
        <div className="flex gap-3">
          <button onClick={() => setOpenGenModal(true)} className="px-4 py-2 bg-bg-card border border-border text-text-primary rounded-lg font-medium hover:bg-bg-hover transition-colors text-sm">
            Generate Strings
          </button>
          <button onClick={() => setOpenUserModal(true)} className="px-4 py-2 bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors text-sm">
            + New User
          </button>
        </div>
      </div>

      <DataTable data={users ?? []} columns={columns} keyField="username" />

      {/* New User Modal */}
      <Modal open={openUserModal} onClose={() => setOpenUserModal(false)} title="Add User">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Username</label>
            <input type="text" value={username} onChange={e => setUsername(e.target.value)} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg" />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Password</label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg" />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Device Binding (Optional IP)</label>
            <input type="text" value={binding} onChange={e => setBinding(e.target.value)} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg" />
          </div>
          <div className="mt-6 flex justify-end gap-3">
            <button onClick={() => setOpenUserModal(false)} className="px-4 py-2 text-sm text-text-secondary hover:bg-bg-hover rounded-lg">Cancel</button>
            <button onClick={handleCreateUser} className="px-4 py-2 text-sm bg-accent text-white rounded-lg hover:bg-accent-hover">Save</button>
          </div>
        </div>
      </Modal>

      {/* Generate Strings Modal */}
      <Modal open={openGenModal} onClose={() => setOpenGenModal(false)} title="Generate Proxy Strings">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-text-secondary mb-1">Auth Type</label>
              <select value={genAuthType} onChange={e => setGenAuthType(e.target.value)} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg text-sm">
                <option value="userpass">User:Pass (Muxing)</option>
                <option value="port">Port-based (Dedicated)</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-text-secondary mb-1">Strings per IP</label>
              <input type="number" min={1} value={genCount} onChange={e => setGenCount(Number(e.target.value))} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg text-sm" />
            </div>
          </div>
          {genAuthType === 'port' && (
            <div>
              <label className="block text-sm font-medium text-text-secondary mb-1">Starting Port</label>
              <input type="number" min={1} value={genPort} onChange={e => setGenPort(Number(e.target.value))} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg text-sm" />
            </div>
          )}
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Server IPs (One per line)</label>
            <textarea rows={3} value={genIps} onChange={e => setGenIps(e.target.value)} className="w-full px-4 py-2 bg-bg-input border border-border rounded-lg font-mono text-sm resize-y" placeholder="1.2.3.4&#10;5.6.7.8" />
          </div>
          
          {generatedStrings.length > 0 && (
            <div className="mt-4 border-t border-border pt-4">
              <div className="flex justify-between items-center mb-2">
                <h4 className="text-sm font-medium text-text-primary">Preview ({generatedStrings.length})</h4>
                <CopyButton text={generatedStrings.join('\n')} className="bg-bg-card border border-border" />
              </div>
              <textarea 
                readOnly 
                value={generatedStrings.join('\n')} 
                className="w-full px-3 py-2 bg-[#000] border border-border rounded-lg font-mono text-xs text-text-secondary h-32 resize-none"
              />
            </div>
          )}
        </div>
      </Modal>
    </div>
  )
}
