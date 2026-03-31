import { Routes, Route } from 'react-router-dom'
import MainLayout from './components/Layout/MainLayout'
import Devices from './pages/Devices'
import SlotMonitor from './pages/SlotMonitor'
import ProxyUsers from './pages/ProxyUsers'
import DestinationStats from './pages/DestinationStats'
import Config from './pages/Config'
import Logs from './pages/Logs'

export default function App() {
  return (
    <MainLayout>
      <Routes>
        <Route path="/" element={<Devices />} />
        <Route path="/slots" element={<SlotMonitor />} />
        <Route path="/users" element={<ProxyUsers />} />
        <Route path="/destinations" element={<DestinationStats />} />
        <Route path="/config" element={<Config />} />
        <Route path="/logs" element={<Logs />} />
      </Routes>
    </MainLayout>
  )
}
