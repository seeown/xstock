import { useEffect, useState } from 'react'

// ─── 语音宠物：雷达新票播报 ───────────────────────────────────
// 红牛机器人蹲在右下角。默认睡觉——浏览器要求首次出声必须由
// 用户点击解锁；点击唤醒后值班：新票先"叮"一声再逐句播报，
// 气泡同步显示正在说的内容，说完收起。语音用浏览器原生 TTS，
// 环境不支持时静默降级（形象仍在，仅不出声）。

interface PetState { asleep: boolean; bubble: string; speaking: boolean }

let state: PetState = { asleep: true, bubble: '', speaking: false }
const listeners = new Set<() => void>()
const emit = () => listeners.forEach(l => l())
const set = (patch: Partial<PetState>) => { state = { ...state, ...patch }; emit() }

// ---- 提示音：WebAudio 现场合成（无音频文件依赖）----
let ac: AudioContext | null = null
function ding() {
  try {
    const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) return
    ac = ac ?? new Ctor()
    if (ac.state === 'suspended') void ac.resume()
    const t = ac.currentTime
    const o = ac.createOscillator()
    const g = ac.createGain()
    o.type = 'sine'
    o.frequency.setValueAtTime(880, t)
    o.frequency.exponentialRampToValueAtTime(1318, t + 0.09)
    g.gain.setValueAtTime(0.16, t)
    g.gain.exponentialRampToValueAtTime(0.0001, t + 0.35)
    o.connect(g); g.connect(ac.destination)
    o.start(t); o.stop(t + 0.36)
  } catch { /* 无声环境静默 */ }
}

// ---- 语音引擎：队列逐句，中文嗓音自动挑选 ----
const tts = typeof window !== 'undefined' ? window.speechSynthesis : undefined
let zhVoice: SpeechSynthesisVoice | null = null
const queue: string[] = []

function pickVoice() {
  if (!tts) return
  const vs = tts.getVoices()
  zhVoice = vs.find(v => /zh[-_]CN/i.test(v.lang))
    ?? vs.find(v => v.lang.toLowerCase().startsWith('zh'))
    ?? null
}
if (tts) {
  pickVoice()
  tts.addEventListener?.('voiceschanged', pickVoice)
}

function next() {
  if (!tts) { queue.length = 0; set({ speaking: false, bubble: '' }); return }
  const text = queue.shift()
  if (text == null) { set({ speaking: false, bubble: '' }); return }
  set({ speaking: true, bubble: text })
  const u = new SpeechSynthesisUtterance(text)
  u.lang = 'zh-CN'
  if (zhVoice) u.voice = zhVoice
  u.rate = 1.05
  u.onend = () => window.setTimeout(next, 380) // 句间小停顿
  u.onerror = () => window.setTimeout(next, 120)
  tts.speak(u)
}

export function isAwake() { return !state.asleep }
export function wakePet() { ding(); set({ asleep: false }) }
export function sleepPet() {
  queue.length = 0
  tts?.cancel()
  set({ asleep: true, speaking: false, bubble: '' })
}
/** 入队一句播报（睡着时静默丢弃） */
export function queueSpeech(text: string) {
  if (state.asleep || !text) return
  queue.push(text)
  if (!state.speaking) next()
}
/** 新票提示音（值班中才响） */
export function dingForPick() { if (!state.asleep) ding() }

export function usePetState(): PetState {
  const [, bump] = useState(0)
  useEffect(() => {
    const l = () => bump(n => n + 1)
    listeners.add(l)
    return () => { listeners.delete(l) }
  }, [])
  return state
}

// 右下角常驻形象：睡觉灰暗（Zzz），值班带 ON 徽标；说话时弹跳 + 气泡。
export function PetSpeaker() {
  const pet = usePetState()
  return (
    <div className="pet-dock">
      {pet.bubble && <div className="pet-bubble" aria-live="polite">{pet.bubble}</div>}
      <button
        className={`pet-avatar${pet.asleep ? ' asleep' : ''}${pet.speaking ? ' talk' : ''}`}
        onClick={pet.asleep ? wakePet : sleepPet}
        title={pet.asleep ? '点击唤醒：新票语音播报' : '点击休眠：暂停语音播报'}
        aria-label={pet.asleep ? '唤醒语音宠物' : '让语音宠物休眠'}
      >
        <img src="/pet.svg" alt="" draggable={false} />
        {pet.asleep
          ? <span className="pet-zzz" aria-hidden>zZ</span>
          : <span className="pet-badge" aria-hidden>ON</span>}
      </button>
    </div>
  )
}
