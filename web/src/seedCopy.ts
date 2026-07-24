import type { Seed } from './types'

export function formatSeedCopyText(seed: Pick<Seed, 'id' | 'title' | 'content'>) {
  return `事种ID: ${seed.id}\n标题：${seed.title}\n描述：${seed.content}`
}
