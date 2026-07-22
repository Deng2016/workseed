export type SeedType = 'idea' | 'feature' | 'todo' | 'bug'
export type SeedStatus = 'inbox' | 'doing' | 'paused' | 'skipped' | 'done'
export type SeedPriority = 'high' | 'middle' | 'low'

export interface Project {
  id: number
  name: string
  description: string
  createdAt?: string
  archived: boolean
  seedCount: number
}

export interface AppSettings {
  workdayStart: string
  workdayEnd: string
}

export interface SeedCounts {
  total: number
  idea: number
  feature: number
  todo: number
  bug: number
  inbox: number
  doing: number
  paused: number
  skipped: number
  done: number
  high: number
  middle: number
  low: number
}

export interface SeedListResult {
  items: Seed[]
  counts: SeedCounts
  page: number
  pageSize: number
  total: number
  hasMore: boolean
}

export interface Seed {
  id: number
  projectId: number
  type: SeedType
  status: SeedStatus
  title: string
  content: string
  priority: SeedPriority
  createdAt?: string
  updatedAt?: string
  startedAt?: string | null
  completedAt?: string | null
  durationSeconds?: number | null
}
