# frontend

React + TypeScript (Vite) frontend for DevPlatform. Talks to the Go backend's
JSON API under `/api/*` (see `../backend`).

## Development

```
npm install
npm run dev
```

The dev server proxies `/api` and `/healthz` to `http://localhost:8080`
(see `vite.config.ts`), so run the backend alongside it:

```
cd ../backend && go run ./cmd/devplatform
```

## Auth

There's no login form backed by a real credential check here — per the
design doc, real authentication happens in an existing external system
(itself backed by AD/LDAP). That system is expected to hand off to this
app with a signed JWT as a `?token=` query parameter; `AuthProvider`
(`src/auth/AuthContext.tsx`) reads it once on load, stores it, and strips
it from the URL. The login page's manual "paste a token" field is a
dev/test fallback for exercising the app before that handoff exists.

## Structure

```
src/api/         Typed fetch client + JSON shapes mirroring the Go API
src/auth/        Token handoff, current-user context
src/components/  Shared chrome (top bar, route guard)
src/pages/       One file per route
```

## Build

```
npm run build
```

Type-checks (`tsc -b`) then produces `dist/`. Per the design doc, the Go
backend is meant to eventually embed this build output into its own binary
rather than serving it separately — that wiring doesn't exist yet.
