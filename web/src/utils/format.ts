// credits 计量单位相关工具函数

// credits 单位常量：1 credit = 10^9 nano credits
export const CREDITS_UNIT = 1e9

// 存储（nano credits/1K）转显示（credits/1M）的换算因子
export const CREDITS_PER_MILLION = 1e6

/**
 * 格式化 credits 显示（通用，用于成本、消费等）
 * @param nano nano credits 值
 */
export function formatCredits(nano: number): string {
  if (nano === 0) return '0 credits'
  const credits = nano / CREDITS_UNIT
  if (credits >= 1) {
    return credits.toFixed(credits < 10 ? 4 : 2) + ' credits'
  } else if (credits >= 0.001) {
    return (credits * 1000).toFixed(2) + ' mcredits'
  } else if (credits >= 0.000001) {
    return (credits * 1000000).toFixed(2) + ' microcredits'
  }
  return nano + ' nano credits'
}

/**
 * 格式化价格显示（credits/M tokens）
 * @param storageValue 存储（nano credits/1K）
 */
export function formatPricePerM(storageValue: number): string {
  if (storageValue === 0) return '0 credits/M'
  const credits = storageValue / CREDITS_PER_MILLION
  if (credits >= 1) {
    return credits.toFixed(credits < 10 ? 4 : 2) + ' credits/M'
  } else if (credits >= 0.001) {
    return (credits * 1000).toFixed(2) + ' mcredits/M'
  } else if (credits >= 0.000001) {
    return (credits * 1000000).toFixed(2) + ' microcredits/M'
  }
  return (credits * 1e9).toFixed(0) + ' nano credits/M'
}

/**
 * 存储（nano credits/1K）转显示（credits/1M）
 */
export function storageToDisplay(storageValue: number): number {
  return storageValue / CREDITS_PER_MILLION
}

/**
 * 显示（credits/1M）转存储（nano credits/1K）
 */
export function displayToStorage(displayValue: number): number {
  return Math.round(displayValue * CREDITS_PER_MILLION)
}
