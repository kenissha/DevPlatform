import type {
  AccessRegistry,
  AuditEvent,
  Commit,
  Contributor,
  DayCount,
  DeployRecipe,
  DeployTarget,
  DeploymentRequest,
  DeploymentStatus,
  DiffResult,
  DisplayNameRegistry,
  MergeRequest,
  MergeRequestDetail,
  MergeRequestStatus,
  Notification,
  Person,
  ReleaseInfo,
  Task,
  TaskStatus,
  User,
} from './types'

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
  listPeople: () => request<Person[]>('/api/users'),

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

  // Cross-repo views, for the dashboard.
  listAllTasks: (assignedTo?: string) =>
    request<Task[]>(`/api/tasks${assignedTo ? `?assignedTo=${encodeURIComponent(assignedTo)}` : ''}`),
  listAllMergeRequests: (status?: MergeRequestStatus) =>
    request<MergeRequest[]>(`/api/merge-requests${status ? `?status=${status}` : ''}`),

  // Repository insight.
  listCommits: (repo: string, limit = 20) =>
    request<Commit[]>(`/api/repos/${encodeURIComponent(repo)}/commits?limit=${limit}`),
  listContributors: (repo: string) =>
    request<Contributor[]>(`/api/repos/${encodeURIComponent(repo)}/contributors`),
  activity: (repo: string, days = 30) =>
    request<DayCount[]>(`/api/repos/${encodeURIComponent(repo)}/activity?days=${days}`),

  listAudit: (limit = 100) => request<AuditEvent[]>(`/api/audit?limit=${limit}`),

  listNotifications: () => request<Notification[]>('/api/notifications'),
  markNotificationRead: (id: string) =>
    request<void>(`/api/notifications/${encodeURIComponent(id)}/read`, { method: 'POST' }),

  listDeployTargetEnvironments: (repo: string) =>
    request<string[]>(`/api/repos/${encodeURIComponent(repo)}/deploy-targets`),
  listDeployments: (repo: string) =>
    request<DeploymentRequest[]>(`/api/repos/${encodeURIComponent(repo)}/deployments`),
  createDeployment: (repo: string, environment: string, sourceBranch: string) =>
    request<DeploymentRequest>(`/api/repos/${encodeURIComponent(repo)}/deployments`, {
      method: 'POST',
      body: JSON.stringify({ environment, sourceBranch }),
    }),
  getDeployment: (repo: string, id: string) =>
    request<DeploymentRequest>(`/api/repos/${encodeURIComponent(repo)}/deployments/${encodeURIComponent(id)}`),
  approveDeployment: (repo: string, id: string) =>
    request<DeploymentRequest>(
      `/api/repos/${encodeURIComponent(repo)}/deployments/${encodeURIComponent(id)}/approve`,
      { method: 'POST' },
    ),
  rejectDeployment: (repo: string, id: string) =>
    request<DeploymentRequest>(
      `/api/repos/${encodeURIComponent(repo)}/deployments/${encodeURIComponent(id)}/reject`,
      { method: 'POST' },
    ),
  listAllDeployments: (status?: DeploymentStatus) =>
    request<DeploymentRequest[]>(`/api/deployments${status ? `?status=${status}` : ''}`),

  // Rollback (Admin-only on the backend, see backend/internal/deployment's
  // Releases/Rollback). Releases lists what's still on disk for one
  // (repo, environment) deploy target; rollback repoints IIS at one of
  // them immediately, no approval stage — see the design conversation
  // this shipped from for why.
  listReleases: (repo: string, environment: string) =>
    request<ReleaseInfo[]>(
      `/api/repos/${encodeURIComponent(repo)}/deployments/${encodeURIComponent(environment)}/releases`,
    ),
  rollback: (repo: string, environment: string, release: string) =>
    request<DeploymentRequest>(
      `/api/repos/${encodeURIComponent(repo)}/deployments/${encodeURIComponent(environment)}/rollback`,
      { method: 'POST', body: JSON.stringify({ release }) },
    ),

  // Deploy-target management (Admin-only on the backend). siteName is
  // validated server-side against the ops-managed allow-list — see
  // listAllowedSites — never accepted as free text.
  listDeployTargets: () => request<DeployTarget[]>('/api/deploy-targets'),
  setDeployTarget: (repo: string, environment: string, target: Omit<DeployTarget, 'repo' | 'environment'>) =>
    request<DeployTarget>(
      `/api/deploy-targets/${encodeURIComponent(repo)}/${encodeURIComponent(environment)}`,
      { method: 'PUT', body: JSON.stringify(target) },
    ),
  deleteDeployTarget: (repo: string, environment: string) =>
    request<void>(
      `/api/deploy-targets/${encodeURIComponent(repo)}/${encodeURIComponent(environment)}`,
      { method: 'DELETE' },
    ),
  listAllowedSites: () => request<string[]>('/api/allowed-sites'),

  // Per-project authorization (Admin-only on the backend). A subject
  // absent from listAccess's result is unrestricted — see AccessRegistry.
  listAccess: () => request<AccessRegistry>('/api/access'),
  setAccess: (subject: string, repos: string[]) =>
    request<{ repos: string[] }>(`/api/access/${encodeURIComponent(subject)}`, {
      method: 'PUT',
      body: JSON.stringify({ repos }),
    }),
  clearAccess: (subject: string) =>
    request<void>(`/api/access/${encodeURIComponent(subject)}`, { method: 'DELETE' }),

  // Per-person display-name override (Admin-only on the backend). A
  // subject absent from listDisplayNames's result falls back to their
  // email — see DisplayNameRegistry and User.displayName.
  listDisplayNames: () => request<DisplayNameRegistry>('/api/display-names'),
  setDisplayName: (subject: string, name: string) =>
    request<{ name: string }>(`/api/display-names/${encodeURIComponent(subject)}`, {
      method: 'PUT',
      body: JSON.stringify({ name }),
    }),
  clearDisplayName: (subject: string) =>
    request<void>(`/api/display-names/${encodeURIComponent(subject)}`, { method: 'DELETE' }),

  // Per-person git credential (Admin-only revoke; anyone can mint their
  // own — see backend/internal/gittoken). The raw token in
  // generateGitToken's response is shown to the caller exactly once;
  // DevPlatform never stores or re-displays it.
  generateGitToken: () => request<{ token: string }>('/api/me/git-token', { method: 'POST' }),
  revokeGitToken: (subject: string) =>
    request<void>(`/api/git-token/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
}

export type {
  AccessRegistry,
  AuditEvent,
  Commit,
  Contributor,
  DayCount,
  DeployRecipe,
  DeployTarget,
  DeploymentRequest,
  DiffResult,
  DisplayNameRegistry,
  MergeRequest,
  MergeRequestDetail,
  Notification,
  Person,
  ReleaseInfo,
  Task,
  User,
}
