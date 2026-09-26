import { useEffect } from 'react'

export interface ShortcutMap {
  [key: string]: (e: KeyboardEvent) => void
}

// 全局键盘快捷键：j/k 行移动、Enter 详情、/ 搜索、r 刷新（接入指南约定）。
// 输入框 / 文本域聚焦时自动跳过。
export function useKeyboardShortcuts(map: ShortcutMap, enabled = true) {
  useEffect(() => {
    if (!enabled) return
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
  }, [map, enabled])
}
