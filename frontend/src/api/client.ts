import type { DiffResult, MergeRequest, MergeRequestDetail, Task, TaskStatus, User } from './types'

// Thrown by request() on any non-2xx response, so callers/pages can
// distinguish "not logged in" (401) from "not allowed" (403) from
// "doesn't exist" (404) instead of just showing a generic failure.
export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

let authToken: string | null = null

// Called by AuthProvider whenever the token changes, so every request()
// call below picks up the current token without every call site having to
// thread it through manually.
export function setAuthToken(token: string | null) {
  authToken = token
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (authToken) {
    headers.set('Authorization', `Bearer ${authToken}`)
  }
  if (init?.body) {
    headers.set('Content-Type', 'application/json')
  }

  const res = await fetch(path, { ...init, headers })
  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(res.status, text || `request to ${path} failed with ${res.status}`)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export const api = {
  me: () => request<User>('/api/me'),

  listRepos: () => request<string[]>('/api/repos'),
  createRepo: (name: string) =>
    request<{ name: string }>('/api/repos', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
  listBranches: (repo: string) => request<string[]>(`/api/repos/${encodeURIComponent(repo)}/branches`),

  listMergeRequests: (repo: string) =>
    request<MergeRequest[]>(`/api/repos/${encodeURIComponent(repo)}/merge-requests`),
  createMergeRequest: (repo: string, title: string, sourceBranch: string, targetBranch: string) =>
    request<MergeRequest>(`/api/repos/${encodeURIComponent(repo)}/merge-requests`, {
      method: 'POST',
      body: JSON.stringify({ title, sourceBranch, targetBranch }),
    }),
  getMergeRequest: (repo: string, id: string) =>
    request<MergeRequestDetail>(`/api/repos/${encodeURIComponent(repo)}/merge-requests/${encodeURIComponent(id)}`),
  approveMergeRequest: (repo: string, id: string) =>
    request<MergeRequest>(
      `/api/repos/${encodeURIComponent(repo)}/merge-requests/${encodeURIComponent(id)}/approve`,
      { method: 'POST' },
    ),
  rejectMergeRequest: (repo: string, id: string) =>
    request<MergeRequest>(
      `/api/repos/${encodeURIComponent(repo)}/merge-requests/${encodeURIComponent(id)}/reject`,
      { method: 'POST' },
    ),

  listTasks: (repo: string) => request<Task[]>(`/api/repos/${encodeURIComponent(repo)}/tasks`),
  createTask: (repo: string, title: string, description: string, assignedTo: string) =>
    request<Task>(`/api/repos/${encodeURIComponent(repo)}/tasks`, {
      method: 'POST',
      body: JSON.stringify({ title, description, assignedTo }),
    }),
  updateTask: (
    repo: string,
    id: string,
    changes: Partial<{ status: TaskStatus; urgent: boolean; assignedTo: string }>,
  ) =>
    request<Task>(`/api/repos/${encodeURIComponent(repo)}/tasks/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(changes),
    }),
}

export type { DiffResult, MergeRequest, MergeRequestDetail, Task, User }
