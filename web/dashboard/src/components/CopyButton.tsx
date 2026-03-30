import { useState } from 'react'

interface CopyButtonProps {
  text: string
  label?: string
  className?: string
}

export default function CopyButton({ text, label = 'Copy', className = '' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className={`px-3 py-1.5 rounded-lg text-xs font-semibold transition-all ${
        copied
          ? 'bg-success-muted text-success'
          : 'bg-bg-hover text-text-secondary hover:bg-border hover:text-text-primary'
      } ${className}`}
    >
      {copied ? 'Copied!' : label}
    </button>
  )
}
