import { parseTiersFromExpr } from '@/features/pricing/lib/billing-expr'

export function isGptLongContextWarningModel(
  modelName: string | undefined
): boolean {
  const name = modelName?.trim().toLowerCase() ?? ''
  if (!name.startsWith('gpt-')) return false
  return !/(^|-)(image|audio|video)($|-)/.test(name)
}

/**
 * Return the first input-length boundary in a tiered billing expression.
 * Invalid expressions and expressions without `len` conditions are deliberately
 * treated as having no warning threshold rather than guessing from a model.
 */
export function getTieredInputTokenThreshold(expression: string): number | null {
  const tiers = parseTiersFromExpr(expression)
  const thresholds = tiers.flatMap((tier) =>
    tier.conditions
      .filter((condition) => condition.var === 'len')
      .map((condition) => condition.value)
      .filter((value) => Number.isFinite(value) && value > 0)
  )
  return thresholds.length > 0 ? Math.min(...thresholds) : null
}
