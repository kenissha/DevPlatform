// cloneURL builds a repository's git address from the page's own origin
// rather than from a configured hostname.
//
// Whoever is reading the page reached the platform at that origin, so it
// is by definition an address that works for them. There is no server-side
// setting to keep in sync, and it stays correct behind IIS's reverse
// proxy, where the backend only ever sees an internal loopback address as
// its own Host (the same trap that broke the login installer — see
// docs/DURUM.md's 2026-09-03 entry).
//
// The "/git" prefix must stay in sync with gitserver.Prefix on the
// backend.
export function cloneURL(repo: string): string {
  return `${window.location.origin}/git/${encodeURIComponent(repo)}.git`
}
