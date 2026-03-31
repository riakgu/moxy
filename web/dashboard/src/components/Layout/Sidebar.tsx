import { NavLink } from 'react-router-dom'

const navItems = [
  { to: '/', label: 'Devices', icon: '📱' },
  { to: '/slots', label: 'Slot Monitor', icon: '🖥️' },
  { to: '/users', label: 'Proxy Users', icon: '👤' },
  { to: '/destinations', label: 'Destinations', icon: '🌐' },
  { to: '/config', label: 'Config', icon: '⚙️' },
  { to: '/logs', label: 'Logs', icon: '📋' },
]

interface SidebarProps {
  healthySlots: number
  totalSlots: number
}

export default function Sidebar({ healthySlots, totalSlots }: SidebarProps) {
  return (
    <aside className="fixed left-0 top-0 h-screen w-60 bg-bg-sidebar border-r border-border flex flex-col z-40">
      {/* Logo */}
      <div className="px-5 py-6 border-b border-border">
        <h1 className="text-xl font-bold text-text-primary tracking-tight">
          <span className="text-accent">⬡</span> Moxy
        </h1>
      </div>

      {/* Navigation */}
      <nav className="flex-1 py-4 px-3 space-y-1">
        {navItems.map(item => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-accent-muted text-accent'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover'
              }`
            }
          >
            <span className="text-base">{item.icon}</span>
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* Health indicator */}
      <div className="px-5 py-4 border-t border-border">
        <div className="flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${healthySlots > 0 ? 'bg-success' : 'bg-danger'}`} />
          <span className="text-xs text-text-secondary">
            {healthySlots}/{totalSlots} healthy
          </span>
        </div>
      </div>
    </aside>
  )
}
