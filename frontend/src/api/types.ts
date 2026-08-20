// Mirrors the JSON shapes served by the Go backend (see
// backend/internal/auth, backend/internal/repoapi,
// backend/internal/mergerequest, backend/internal/taskboard). Keep these
// in sync by hand — there is no codegen step, the backend is small enough
// that drift is easy to spot.

export type Role = 'admin' | 'developer'

export interface User {
  subject: string
  email: string
  role: Role
}

export type MergeRequestStatus = 'open' | 'approved' | 'rejected'

export interface MergeRequest {
  id: string
  repo: string
  title: string
  sourceBranch: string
  targetBranch: string
  author: string
  status: MergeRequestStatus
  createdAt: string
  mergedCommit?: string
}

export interface FileStat {
  name: string
  addition: number
  deletion: number
}

export interface DiffResult {
  unifiedDiff: string
  stats: FileStat[]
}

export interface MergeRequestDetail extends MergeRequest {
  diff: DiffResult
}

// A person the platform has seen. Created just-in-time on their first
// authenticated request (see backend/internal/users), so this list is
// "who has access", not "who was invited".
export interface Person {
  subject: string
  email: string
  role: Role
  firstSeen: string
  lastSeen: string
}

export type AuditAction =
  | 'repo.created'
  | 'task.created'
  | 'task.updated'
  | 'merge_request.opened'
  | 'merge_request.approved'
  | 'merge_request.rejected'
  | 'deployment.opened'
  | 'deployment.deployed'
  | 'deployment.failed'
  | 'deployment.rejected'

export interface AuditEvent {
  at: string
  actor: string
  action: AuditAction
  repo?: string
  target?: string
  summary?: string
}

export interface Commit {
  hash: string
  shortHash: string
  message: string
  authorName: string
  authorEmail: string
  when: string
}

export interface Contributor {
  name: string
  email: string
  commits: number
  lastAt: string
}

export interface DayCount {
  date: string
  commits: number
}

export type TaskStatus = 'in_progress' | 'awaiting_test' | 'done'

export interface Task {
  id: string
  repo: string
  title: string
  description: string
  assignedTo: string
  author: string
  status: TaskStatus
  urgent: boolean
  createdAt: string
}

// kind is a bare string, not a union: the backend (backend/internal/notify)
// treats it as an opaque tag its producers choose (currently
// "task_assigned", "merge_request_opened") — new kinds can ship on the
// backend without a frontend type change, same reasoning as AuditEvent's
// optional fields. NOTIFICATION_KIND_LABELS in labels.ts falls back to the
// raw kind for anything it doesn't recognise.
export type DeploymentStatus = 'pending' | 'deployed' | 'failed' | 'rejected'

// A request to release repo's sourceBranch into environment — mirrors
// backend/internal/deployment.Request. Approving one actually runs the
// build+version+IIS-swap pipeline, so releaseDir/failureReason are only
// populated once Status has left "pending".
//
// kind is unset for an ordinary deploy request; a rollback (see
// ReleaseInfo/rollback below) sets it to 'rollback' — that record has no
// sourceBranch (a rollback repoints IIS at a release an earlier deploy
// already built, it doesn't build anything new) and is already terminal
// (status 'deployed') the moment it's created, with no 'pending' stage.
export interface DeploymentRequest {
  id: string
  repo: string
  environment: string
  kind?: 'rollback'
  sourceBranch?: string
  author: string
  status: DeploymentStatus
  releaseDir?: string
  failureReason?: string
  createdAt: string
  decidedAt?: string
}

// One release still on disk for a (repo, environment) deploy target —
// mirrors backend/internal/deployment.releaseInfo. What the rollback
// picker on the Deploy Hedefleri page lists.
export interface ReleaseInfo {
  name: string
  active: boolean
}

export type DeployRecipe = 'dotnet' | 'npm' | 'go'

// One (repo, environment) pair's deploy configuration — mirrors
// backend/internal/deployment.Target. Managed from the panel's "Deploy
// Hedefleri" page (Admin-only); siteName must be one of
// GET /api/allowed-sites's values, enforced server-side.
export interface DeployTarget {
  repo: string
  environment: string
  recipe: DeployRecipe
  siteName: string
  secretsTarget?: string
  keepVersions: number
}

export interface Notification {
  id: string
  recipient: string
  kind: string
  message: string
  link: string
  read: boolean
  createdAt: string
}

// Maps subject -> the repos they're restricted to (see
// backend/internal/access). A subject absent from this map is
// unrestricted — they can see every repo. This is the design doc's Faz 3
// "proje bazlı yetkilendirme".
export type AccessRegistry = Record<string, string[]>
