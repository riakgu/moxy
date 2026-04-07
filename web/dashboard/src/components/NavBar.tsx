import { NavLink } from 'react-router-dom'

const navLinks = [
  { to: '/', label: 'Dashboard' },
  { to: '/logs', label: 'Logs' },
  { to: '/config', label: 'Config' },
]

export default function NavBar() {
  return (
    <nav className="sticky top-0 z-50 bg-bg-surface/90 backdrop-blur-sm border-b border-border-subtle">
      <div className="max-w-7xl mx-auto px-6 h-14 flex items-center justify-between">
        {/* Logo */}
        <div className="flex items-center gap-8">
          <span className="font-mono text-xl font-semibold tracking-widest">
            <span className="text-accent-cyan">M</span>
            <span className="text-text-primary">OXY</span>
          </span>

          {/* Nav links */}
          <div className="flex items-center gap-1">
            {navLinks.map((link) => (
              <NavLink
                key={link.to}
                to={link.to}
                end
                className={({ isActive }) =>
                  `px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                    isActive
                      ? 'text-accent-cyan bg-accent-cyan/10'
                      : 'text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover'
                  }`
                }
              >
                {link.label}
              </NavLink>
            ))}
          </div>
        </div>

        {/* Status indicator */}
        <div className="flex items-center gap-2 text-xs text-text-muted font-mono">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse-badge" />
          SYSTEM ONLINE
        </div>
      </div>
    </nav>
  )
}
