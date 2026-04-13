import { useState, useEffect } from 'react'
import { NavLink, useLocation } from 'react-router-dom'

const navLinks = [
  { to: '/', label: 'Dashboard', icon: '◈' },
  { to: '/traffic', label: 'Traffic', icon: '◆' },
  { to: '/logs', label: 'Logs', icon: '▣' },
  { to: '/system', label: 'System', icon: '⚙' },
  { to: '/config', label: 'Config', icon: '☰' },
]

export default function NavBar() {
  const [open, setOpen] = useState(false)
  const location = useLocation()

  // Close menu on route change
  useEffect(() => {
    setOpen(false)
  }, [location.pathname])

  // Prevent body scroll when menu is open
  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  return (
    <nav className="sticky top-0 z-50 bg-bg-surface/90 backdrop-blur-sm border-b border-border-subtle">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 h-14 flex items-center justify-between">
        {/* Logo */}
        <span className="font-mono text-xl font-semibold tracking-widest">
          <span className="text-accent-cyan">M</span>
          <span className="text-text-primary">OXY</span>
        </span>

        {/* Desktop nav links */}
        <div className="hidden sm:flex items-center gap-1">
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

        {/* Desktop status */}
        <div className="hidden sm:flex items-center gap-2 text-xs text-text-muted font-mono">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse-badge" />
          SYSTEM ONLINE
        </div>

        {/* Mobile hamburger button */}
        <button
          onClick={() => setOpen(!open)}
          className="sm:hidden relative w-8 h-8 flex items-center justify-center cursor-pointer"
          aria-label={open ? 'Close menu' : 'Open menu'}
        >
          <span className={`hamburger-line hamburger-top ${open ? 'hamburger-open' : ''}`} />
          <span className={`hamburger-line hamburger-mid ${open ? 'hamburger-open' : ''}`} />
          <span className={`hamburger-line hamburger-bot ${open ? 'hamburger-open' : ''}`} />
        </button>
      </div>

      {/* Mobile menu overlay */}
      {open && (
        <div
          className="sm:hidden fixed inset-0 top-14 z-40 bg-bg-primary/80 backdrop-blur-sm animate-fade-in"
          onClick={() => setOpen(false)}
        />
      )}

      {/* Mobile menu panel */}
      <div
        className={`sm:hidden fixed top-14 left-0 right-0 z-50 bg-bg-surface border-b border-border-subtle
          transition-all duration-200 ease-out origin-top
          ${open ? 'opacity-100 scale-y-100' : 'opacity-0 scale-y-0 pointer-events-none'}`}
      >
        <div className="px-4 py-3 space-y-1">
          {navLinks.map((link) => (
            <NavLink
              key={link.to}
              to={link.to}
              end
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium font-mono
                  transition-all ${
                  isActive
                    ? 'text-accent-cyan bg-accent-cyan/10 border border-accent-cyan/20'
                    : 'text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover border border-transparent'
                }`
              }
            >
              <span className="text-base">{link.icon}</span>
              {link.label}
            </NavLink>
          ))}
        </div>

        {/* Mobile status footer */}
        <div className="px-4 py-3 border-t border-border-subtle/50 flex items-center gap-2 text-xs text-text-muted font-mono">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse-badge" />
          SYSTEM ONLINE
        </div>
      </div>
    </nav>
  )
}
