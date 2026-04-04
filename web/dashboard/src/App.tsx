import { Outlet } from 'react-router-dom'
import NavBar from './components/NavBar'

export default function App() {
  return (
    <div className="min-h-screen bg-bg-primary">
      <NavBar />
      <main className="max-w-7xl mx-auto px-6 py-8">
        <Outlet />
      </main>
    </div>
  )
}
