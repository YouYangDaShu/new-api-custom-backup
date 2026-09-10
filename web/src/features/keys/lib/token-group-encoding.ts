/*
Encode/decode token.group for multi-group keys (汇流-compatible).

  single: "grok"
  multi:  "@manual:grok,gpt plus 号池,Claude"
  auto:   "auto"  (legacy fixed AutoGroups order)
  smart:  "@smart:balanced" | "@smart:price" | "@smart:success_rate"
*/
export type TokenGroupMode = 'auto' | 'manual' | 'single' | 'smart'

export type SmartRouteMode = 'balanced' | 'price' | 'success_rate'

export type ParsedTokenGroup = {
  mode: TokenGroupMode
  groups: string[]
  raw: string
  smartMode?: SmartRouteMode
}

const MANUAL_PREFIX = '@manual:'
const SMART_PREFIX = '@smart:'

export function isManualTokenGroup(value?: string | null): boolean {
  return !!value && value.startsWith(MANUAL_PREFIX)
}

export function isSmartTokenGroup(value?: string | null): boolean {
  return !!value && value.startsWith(SMART_PREFIX)
}

export function parseSmartMode(value?: string | null): SmartRouteMode {
  const raw = (value || '').trim()
  if (!raw.startsWith(SMART_PREFIX)) return 'balanced'
  const mode = raw.slice(SMART_PREFIX.length).trim().toLowerCase()
  if (mode === 'price' || mode === 'cheap' || mode === 'cost') return 'price'
  if (mode === 'success_rate' || mode === 'success' || mode === 'reliability') {
    return 'success_rate'
  }
  return 'balanced'
}

export function parseTokenGroupValue(value?: string | null): ParsedTokenGroup {
  const raw = (value || '').trim()
  if (!raw) {
    return { mode: 'manual', groups: [], raw: '' }
  }
  if (raw === 'auto') {
    return { mode: 'auto', groups: [], raw }
  }
  if (isSmartTokenGroup(raw)) {
    return {
      mode: 'smart',
      groups: [],
      raw,
      smartMode: parseSmartMode(raw),
    }
  }
  if (isManualTokenGroup(raw)) {
    const groups = raw
      .slice(MANUAL_PREFIX.length)
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s && s !== 'auto' && !s.startsWith('@'))
    const seen = new Set<string>()
    const out: string[] = []
    for (const g of groups) {
      if (seen.has(g)) continue
      seen.add(g)
      out.push(g)
    }
    return { mode: 'manual', groups: out, raw }
  }
  return { mode: 'single', groups: [raw], raw }
}

export function encodeTokenGroupValue(input: {
  mode: TokenGroupMode
  groups: string[]
  smartMode?: SmartRouteMode
}): string {
  if (input.mode === 'auto') return 'auto'
  if (input.mode === 'smart') {
    return SMART_PREFIX + (input.smartMode || 'balanced')
  }
  const seen = new Set<string>()
  const groups: string[] = []
  for (const g of input.groups || []) {
    const name = (g || '').trim()
    if (!name || name === 'auto' || name.startsWith('@')) continue
    if (seen.has(name)) continue
    seen.add(name)
    groups.push(name)
  }
  if (groups.length === 0) return ''
  if (groups.length === 1) return groups[0]
  return MANUAL_PREFIX + groups.join(',')
}
