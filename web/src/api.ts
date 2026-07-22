import type { Project, Seed, SeedListResult, SeedPriority, SeedStatus, SeedType } from './types'

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: '请求失败' }))
    throw new Error(body.error || '请求失败')
  }
  if (response.status === 204) return undefined as T
  return response.json()
}

export const api = {
  version: () => request<{ version: string }>('/api/version'),
  projects: () => request<Project[]>('/api/projects'),
  createProject: (input: Pick<Project, 'name' | 'description'>) =>
    request<Project>('/api/projects', { method: 'POST', body: JSON.stringify(input) }),
  seeds: async (projectId: number, types: SeedType[], statuses: SeedStatus[], priorities: SeedPriority[], page = 1, pageSize = 20): Promise<SeedListResult> => {
    const query = new URLSearchParams({ projectId: String(projectId), page: String(page), pageSize: String(pageSize) })
    const addFilter = (name: string, values: string[]) => {
      if (!values.length) query.set(name, '')
      else values.forEach(value => query.append(name, value))
    }
    addFilter('type', types)
    addFilter('status', statuses)
    addFilter('priority', priorities)
    const url = `/api/seeds?${query}`
    const response = await fetch(url)
    if (!response.ok) {
      const body = await response.json().catch(() => ({ error: "请求失败" }))
      throw new Error(body.error || "请求失败")
    }
    const items = await response.json() as Seed[]
    const readCount = (name: string) => Number(response.headers.get("X-Seed-Count-" + name) || 0)
    return { items, counts: {
      total: readCount("Total"), idea: readCount("Idea"), feature: readCount("Feature"),
      todo: readCount("Todo"), bug: readCount("Bug"), inbox: readCount("Inbox"), doing: readCount("Doing"), done: readCount("Done"),
      high: readCount("High"), middle: readCount("Middle"), low: readCount("Low"),
    }, page: Number(response.headers.get('X-Seed-Page') || page),
      pageSize: Number(response.headers.get('X-Seed-Page-Size') || pageSize),
      total: Number(response.headers.get('X-Seed-Filtered-Total') || items.length),
      hasMore: response.headers.get('X-Seed-Has-More') === 'true',
    }
  },
  createSeed: (input: Omit<Seed, 'id'>) =>
    request<Seed>('/api/seeds', { method: 'POST', body: JSON.stringify(input) }),
  updateSeed: (seed: Seed) =>
    request<Seed>(`/api/seeds/${seed.id}`, { method: 'PATCH', body: JSON.stringify(seed) }),
  deleteSeed: (id: number) => request<void>(`/api/seeds/${id}`, { method: 'DELETE' }),
  worklogs: (startTime: string, endTime: string) => {
    const query = new URLSearchParams({ startTime, endTime })
    return request<Seed[]>(`/api/worklogs?${query}`)
  },
}
