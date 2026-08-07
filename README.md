# DevPlatform

DevPlatform is a small, self-hosted internal platform for teams that can't (or don't want to) hand out real credentials, server access, or unrestricted push/deploy rights to every collaborator — while still keeping full visibility into who is working on what.

It grew out of a simple problem: a solo developer bringing on a second engineer, without wanting to share production secrets, database connection strings, or direct GitHub/server access. Instead of adopting an external tool, DevPlatform is built in-house, tailored exactly to that workflow.

## What it does

- **Self-hosted git hosting** — collaborators push/pull against DevPlatform's own git server, not GitHub directly. The real GitHub repository stays under the owner's sole control and is synced manually, on the owner's schedule.
- **Protected branches, enforced at the protocol level** — direct pushes to protected branches (e.g. `main`) are rejected by the git server itself, not just discouraged by a UI.
- **Task board & request workflow** — tasks are created and tracked (in progress / awaiting test / done, urgent flag), and actions like "build this", "deploy to test", or "merge to main" are submitted as requests that an admin reviews and approves — including a real diff view for merge requests, never a blind approval.
- **Automated, versioned deploys** — once approved, builds and deployments to Test/Production happen automatically, each as a new versioned release (older versions are kept, nothing is overwritten), with one-click rollback.
- **Secrets stay on the server** — real environment configuration (database connections, mail, AD credentials) never leaves the server or enters git history; it's injected only at deploy time from a secrets store the admin controls.
- **Audit log** — every meaningful action (task created, request approved/rejected, deploy, rollback) is recorded.

## Status

Early design/setup stage. See [`docs/superpowers/specs/2026-08-07-dev-platform-design.md`](docs/superpowers/specs/2026-08-07-dev-platform-design.md) for the full design and phased rollout plan.

## Project structure

```
backend/    Go backend — git server, task/request API, build & deploy automation
frontend/   React frontend
docs/       Design specs and planning documents
```

## Tech stack

- **Backend:** Go (single self-contained binary, no external runtime dependencies)
- **Frontend:** React (built and embedded into the backend binary)
- **Auth:** Active Directory (AD/LDAP)
