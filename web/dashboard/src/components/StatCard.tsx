interface StatCardProps {
  label: string
  value: string | number
  accent?: 'default' | 'success' | 'warning' | 'danger'
}

const accents = {
  default: 'border-border',
  success: 'border-success/40',
  warning: 'border-warning/40',
  danger: 'border-danger/40',
}

export default function StatCard({ label, value, accent = 'default' }: StatCardProps) {
  return (
    <div className={`bg-bg-card rounded-xl border ${accents[accent]} p-5`}>
      <p className="text-3xl font-bold text-text-primary">{value}</p>
      <p className="text-sm text-text-secondary mt-1">{label}</p>
    </div>
  )
}
