export const MAX_REWARD_RANKS = 100
export const MAX_REWARD_VALUE = 2_147_483_647

const defaultRewardInputs = ['0', '0', '0']

export function rewardInputsFromApi(rewards: readonly number[]): string[] {
  if (rewards.length === 0) return [...defaultRewardInputs]
  return rewards.map(String)
}

export function parseRewardInputs(inputs: readonly string[]): number[] | null {
  if (inputs.length === 0 || inputs.length > MAX_REWARD_RANKS) return null

  const rewards = inputs.map((input) => Number(input.trim() || '0'))
  if (
    rewards.some(
      (reward) =>
        !Number.isFinite(reward) || reward < 0 || reward > MAX_REWARD_VALUE
    )
  ) {
    return null
  }

  return rewards
}
