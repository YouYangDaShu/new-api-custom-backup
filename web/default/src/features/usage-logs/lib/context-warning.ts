export function isGptLongContextWarningModel(
  modelName: string | undefined
): boolean {
  const name = modelName?.trim().toLowerCase() ?? ''
  if (!name.startsWith('gpt-')) return false
  return !/(^|-)(image|audio|video)($|-)/.test(name)
}
