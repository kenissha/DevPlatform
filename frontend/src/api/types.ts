// Mirrors the JSON shapes served by the Go backend (see
// backend/internal/auth, backend/internal/repoapi,
// backend/internal/mergerequest). Keep these in sync by hand — there is no
// codegen step, the backend is small enough that drift is easy to spot.

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
