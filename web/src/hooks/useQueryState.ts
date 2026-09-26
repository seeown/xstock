import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'

// 筛选条件 ↔ URL query：可分享、可回退（接入指南约定）。
// 用法：const [buyPoint, setBuyPoint] = useQueryState('bp')
export function useQueryState(key: string): [string, (v: string | null) => void] {
  const [params, setParams] = useSearchParams()
  const value = params.get(key) ?? ''
  const setValue = useCallback(
    (v: string | null) => {
      setParams(prev => {
        const next = new URLSearchParams(prev)
        if (v == null || v === '') next.delete(key)
        else next.set(key, v)
        return next
      }, { replace: true })
    },
    [key, setParams],
  )
  return [value, setValue]
}
