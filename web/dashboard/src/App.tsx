import { Outlet } from 'react-router-dom'
import NavBar from './components/NavBar'
import { useSSE } from './hooks/useSSE'

export default function App() {
  const sse = useSSE()

  return (
    <div className="min-h-screen bg-bg-primary">
      <NavBar />
      <main className="max-w-7xl mx-auto px-3 sm:px-6 py-6 sm:py-8">
        <Outlet context={sse} />
      </main>
    </div>
  )
}
