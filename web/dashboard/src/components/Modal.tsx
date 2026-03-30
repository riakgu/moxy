import { useEffect, type ReactNode } from 'react'

interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

export default function Modal({ open, onClose, title, children }: ModalProps) {
  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
      return () => { document.body.style.overflow = '' }
    }
  }, [open])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={(e) => e.target === e.currentTarget && onClose()}
    >
      <div className="bg-bg-card rounded-2xl shadow-2xl max-w-md w-full mx-4 border border-border">
        <div className="p-6 border-b border-border">
          <h3 className="text-xl font-bold text-text-primary">{title}</h3>
        </div>
        <div className="p-6">{children}</div>
      </div>
    </div>
  )
}
