import { Routes, Route } from 'react-router-dom'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<div className="text-white p-8">Moxy Dashboard</div>} />
    </Routes>
  )
}
