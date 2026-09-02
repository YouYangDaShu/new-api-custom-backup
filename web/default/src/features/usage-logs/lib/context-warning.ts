export function isGptLongContextWarningModel(
  modelName: string | undefined
): boolean {
  const name = modelName?.trim().toLowerCase() ?? ''
  if (
    !(
      name === 'gpt-5.4' ||
      name.startsWith('gpt-5.4-') ||
      name === 'gpt-5.5' ||
      name.startsWith('gpt-5.5-')
    )
  )
    {return false}
  return !/(^|-)image($|-)|(^|-)audio($|-)|(^|-)video($|-)/.test(name)
}
