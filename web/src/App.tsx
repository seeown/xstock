import { Route, Routes } from 'react-router-dom'
import Layout from './components/Layout'
import Dashboard from './pages/Dashboard'
import DataManager from './pages/DataManager'
import Market from './pages/Market'
import Screen from './pages/Screen'
import Stocks from './pages/Stocks'
import Strategy from './pages/Strategy'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/screen" element={<Screen />} />
        <Route path="/stocks" element={<Stocks />} />
        <Route path="/market" element={<Market />} />
        <Route path="/data" element={<DataManager />} />
        <Route path="/strategy" element={<Strategy />} />
      </Routes>
    </Layout>
  )
}
