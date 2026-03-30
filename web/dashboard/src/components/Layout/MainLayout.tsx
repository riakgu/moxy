import { type ReactNode } from 'react'
import Sidebar from './Sidebar'
import ToastContainer from '../Toast'
import { usePolling } from '../../hooks/usePolling'
import { useToast } from '../../hooks/useToast'
import { statsApi } from '../../api/stats'

interface MainLayoutProps {
  children: ReactNode
}

export default function MainLayout({ children }: MainLayoutProps) {
  const { data: health } = usePolling(() => statsApi.getHealth(), { intervalMs: 10000 })
  const { toasts, addToast, removeToast } = useToast()

  return (
    <div className="min-h-screen">
      <Sidebar
        healthySlots={health?.healthy_slots ?? 0}
        totalSlots={health?.total_slots ?? 0}
      />
      <main className="ml-60 min-h-screen">
        <div className="p-8">{children}</div>
      </main>
      <ToastContainer toasts={toasts} onRemove={removeToast} />
    </div>
  )
}

// Export toast context for pages to use
export { useToast }
