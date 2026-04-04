import { useState } from 'react'

interface CopyButtonProps {
  text: string
  label?: string
  className?: string
}

export default function CopyButton({ text, label = '📋', className = '' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Fallback for non-HTTPS contexts
      const el = document.createElement('textarea')
      el.value = text
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  return (
    <button
      onClick={handleCopy}
      className={`inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-mono
        transition-all duration-200 cursor-pointer
        ${copied
          ? 'bg-accent-cyan/20 text-accent-cyan animate-copy-flash'
          : 'bg-bg-surface-hover text-text-secondary hover:text-text-primary hover:bg-bg-elevated'
        } ${className}`}
      title={`Copy: ${text}`}
    >
      {copied ? '✓ Copied' : label}
    </button>
  )
}
