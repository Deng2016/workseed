import type { Project, Seed, SeedStatus, SeedType } from './types'

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
  projects: () => request<Project[]>('/api/projects'),
  createProject: (input: Pick<Project, 'name' | 'description'>) =>
    request<Project>('/api/projects', { method: 'POST', body: JSON.stringify(input) }),
  seeds: (projectId: number, type: SeedType | 'all', status: SeedStatus | 'all') =>
    request<Seed[]>(`/api/seeds?projectId=${projectId}&type=${type}&status=${status}`),
  createSeed: (input: Omit<Seed, 'id'>) =>
    request<Seed>('/api/seeds', { method: 'POST', body: JSON.stringify(input) }),
  updateSeed: (seed: Seed) =>
    request<Seed>(`/api/seeds/${seed.id}`, { method: 'PATCH', body: JSON.stringify(seed) }),
  deleteSeed: (id: number) => request<void>(`/api/seeds/${id}`, { method: 'DELETE' }),
}

