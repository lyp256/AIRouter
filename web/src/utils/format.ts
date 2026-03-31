// BU 计量单位相关工具函数

// BU 单位常量：1 BU = 10^9 纳 BU
export const BU_UNIT = 1e9

// 存储（纳 BU/1K）转显示（BU/1M）的换算因子
export const BU_PER_MILLION = 1e6

/**
 * 格式化 BU 显示（通用，用于成本、消费等）
 * @param nano 纳 BU 值
 */
export function formatBU(nano: number): string {
  if (nano === 0) return '0 BU'
  const bu = nano / BU_UNIT
  if (bu >= 1) {
    return bu.toFixed(bu < 10 ? 4 : 2) + ' BU'
  } else if (bu >= 0.001) {
    return (bu * 1000).toFixed(2) + ' mBU'
  } else if (bu >= 0.000001) {
    return (bu * 1000000).toFixed(2) + ' µBU'
  }
  return nano + ' nBU'
}

/**
 * 格式化价格显示（BU/M tokens）
 * @param storageValue 存储（纳 BU/1K）
 */
export function formatPricePerM(storageValue: number): string {
  if (storageValue === 0) return '0 BU/M'
  const bu = storageValue / BU_PER_MILLION
  if (bu >= 1) {
    return bu.toFixed(bu < 10 ? 4 : 2) + ' BU/M'
  } else if (bu >= 0.001) {
    return (bu * 1000).toFixed(2) + ' mBU/M'
  } else if (bu >= 0.000001) {
    return (bu * 1000000).toFixed(2) + ' µBU/M'
  }
  return (bu * 1e9).toFixed(0) + ' nBU/M'
}

/**
 * 存储（纳 BU/1K）转显示（BU/1M）
 */
export function storageToDisplay(storageValue: number): number {
  return storageValue / BU_PER_MILLION
}

/**
 * 显示（BU/1M）转存储（纳 BU/1K）
 */
export function displayToStorage(displayValue: number): number {
  return Math.round(displayValue * BU_PER_MILLION)
}
