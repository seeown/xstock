export type EnvTone = 'strong' | 'neutral' | 'weak'

const envTag: Record<EnvTone, string> = { strong: '强势', neutral: '中性', weak: '弱势' }

export function EnvBanner({ tone, desc, time }: { tone: EnvTone; desc: string; time?: string }) {
  return (
    <div className={`env${tone === 'strong' ? '' : ` ${tone}`}`}>
      <span className="tag">{envTag[tone]}</span>
      <span className="desc">{desc}</span>
      {time && <span className="rt">{time}</span>}
    </div>
  )
}
