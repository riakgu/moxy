import type { Device, Slot } from '../api/types'

interface StatsBarProps {
  devices: Device[]
  slots: Slot[]
}

interface StatItemProps {
  label: string
  value: string | number
  glowClass: string
  delay: number
}

function StatItem({ label, value, glowClass, delay }: StatItemProps) {
  return (
    <div
      className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg p-5 card-glow"
      style={{ animationDelay: `${delay}ms` }}
    >
      <p className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">{label}</p>
      <p className={`font-mono text-3xl font-semibold ${glowClass}`}>{value}</p>
    </div>
  )
}

export default function StatsBar({ devices, slots }: StatsBarProps) {
  const devicesOnline = devices.filter((d) => d.status === 'online').length
  const healthySlots = slots.filter((s) => s.status === 'healthy').length
  const unhealthySlots = slots.filter((s) => s.status === 'unhealthy').length
  const activeConnections = slots.reduce((sum, s) => sum + s.active_connections, 0)

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
      <StatItem
        label="Devices Online"
        value={`${devicesOnline} / ${devices.length}`}
        glowClass="text-accent-cyan glow-cyan"
        delay={0}
      />
      <StatItem
        label="Healthy Slots"
        value={healthySlots}
        glowClass="text-accent-green glow-green"
        delay={50}
      />
      <StatItem
        label="Unhealthy Slots"
        value={unhealthySlots}
        glowClass={unhealthySlots > 0 ? 'text-accent-red glow-red' : 'text-text-muted'}
        delay={100}
      />
      <StatItem
        label="Active Connections"
        value={activeConnections}
        glowClass="text-accent-amber glow-amber"
        delay={150}
      />
    </div>
  )
}
