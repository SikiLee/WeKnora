export function buildHistoricalAgentAnswerEvent(
  content: string,
  isCompleted: boolean,
  isFallback: boolean,
): Record<string, unknown> | undefined {
  if (!content.trim()) return undefined
  return {
    type: 'answer',
    content,
    done: isCompleted,
    ...(isFallback ? { is_fallback: true } : {}),
  }
}
