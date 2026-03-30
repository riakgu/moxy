interface StatusBadgeProps {
  status: 'healthy' | 'unhealthy' | 'discovering' | string
}

const styles: Record<string, string> = {
  healthy: 'bg-success-muted text-success',
  unhealthy: 'bg-danger-muted text-danger',
  discovering: 'bg-warning-muted text-warning',
}

export default function StatusBadge({ status }: StatusBadgeProps) {
  const cls = styles[status] ?? 'bg-bg-hover text-text-secondary'
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold ${cls}`}>
      <span className="w-1.5 h-1.5 rounded-full bg-current" />
      {status}
    </span>
  )
}
