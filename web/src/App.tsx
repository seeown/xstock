import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import Layout from './components/Layout'
import DevUI from './pages/DevUI'
import { LegacyRedirect, Shell, ViewProvider } from './shell'
import { VIEWS } from './views'

// 单壳应用：/ 下是保活 Tab 壳(视图 = ?view=)，菜单悬浮/点击切换；
// 旧路径(/screen 等)带全部 query 重定向进壳；/dev/ui 保持独立路由。
export default function App() {
  return (
    <ViewProvider>
      <Routes>
        <Route path="/" element={<Layout><Shell /></Layout>} />
        {/* 开发用组件样张路由，不进壳 */}
        <Route path="/dev/ui" element={<DevUI />} />
        {/* 旧链接兼容：/screen?stage=b2 → /?view=screen&stage=b2 */}
        {VIEWS.filter(v => v.path !== '/').map(v => (
          <Route key={v.path} path={v.path} element={<LegacyRedirect viewKey={v.key} />} />
        ))}
        <Route path="*" element={<UnknownRedirect />} />
      </Routes>
    </ViewProvider>
  )
}

function UnknownRedirect() {
  const loc = useLocation()
  return <Navigate to={`/${loc.search}`} replace />
}
