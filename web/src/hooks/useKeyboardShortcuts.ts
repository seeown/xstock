import { useContext, useEffect } from 'react'
import { ActiveContext } from '../shell'

export interface ShortcutMap {
  [key: string]: (e: KeyboardEvent) => void
}

// 全局键盘快捷键：j/k 行移动、Enter 详情、/ 搜索、r 刷新（接入指南约定）。
// 输入框 / 文本域聚焦时自动跳过；保活壳里非激活的视图自动静默
// (ActiveContext 门控，新页面无法绕过——防止隐藏页串台按键)。
export function useKeyboardShortcuts(map: ShortcutMap, enabled = true) {
  const active = useContext(ActiveContext)
  useEffect(() => {
    if (!enabled || !active) return
    const handler = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) {
        return
      }
      const fn = map[e.key]
      if (fn) {
        e.preventDefault()
        fn(e)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [map, enabled, active])
}
