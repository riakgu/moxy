import type { Toast as ToastType } from '../hooks/useToast'

interface ToastContainerProps {
  toasts: ToastType[]
  onRemove: (id: string) => void
}

const typeStyles = {
  success: 'border-success/40 bg-success-muted text-success',
  error: 'border-danger/40 bg-danger-muted text-danger',
  info: 'border-accent/40 bg-accent-muted text-accent',
}

export default function ToastContainer({ toasts, onRemove }: ToastContainerProps) {
  if (toasts.length === 0) return null

  return (
    <div className="fixed top-4 right-4 z-50 flex flex-col gap-2 max-w-sm pointer-events-none">
      {toasts.map(toast => (
        <div
          key={toast.id}
          className={`border rounded-xl px-4 py-3 shadow-lg text-sm pointer-events-auto cursor-pointer flex items-center gap-3 animate-[slideIn_0.2s_ease-out] ${typeStyles[toast.type]}`}
          onClick={() => onRemove(toast.id)}
        >
          <div className="flex-1 font-medium">{toast.message}</div>
          <button className="text-current opacity-70 hover:opacity-100 transition-opacity">
            ✕
          </button>
        </div>
      ))}
    </div>
  )
}
