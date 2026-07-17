export type SeedType = 'idea' | 'feature' | 'todo' | 'bug'
export type SeedStatus = 'inbox' | 'planned' | 'done' | 'archived'

export interface Project {
  id: number
  name: string
  description: string
  createdAt?: string
}

export interface Seed {
  id: number
  projectId: number
  type: SeedType
  status: SeedStatus
  title: string
  content: string
  priority: number
  createdAt?: string
  updatedAt?: string
}

