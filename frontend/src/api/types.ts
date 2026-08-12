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
export interface DeploymentRequest {
  id: string
  repo: string
  environment: string
  sourceBranch: string
  author: string
  status: DeploymentStatus
  releaseDir?: string
  failureReason?: string
  createdAt: string
  decidedAt?: string
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
