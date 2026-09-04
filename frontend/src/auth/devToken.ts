// Signs a throwaway HS256 JWT entirely client-side, for local
// development only — see LoginPage.tsx's "Geliştirme girişi" section.
// Uses the same default JWT secret the Go backend falls back to when
// DEVPLATFORM_JWT_SECRET isn't set (see backend/internal/config.Load's
// doc comment) — this only ever produces a token the backend accepts
// when it's still running with that default, which is exactly the
// "not a real secret, local dev only" posture the name already
// promises. Never imported outside an `import.meta.env.DEV` branch, so
// Vite's production build tree-shakes this whole module out.
const DEV_JWT_SECRET = 'dev-not-a-real-secret'

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function base64urlFromString(s: string): string {
  return base64url(new TextEncoder().encode(s))
}

// signDevToken mints a token with the exact claim shape
// backend/internal/auth.parseAndValidate expects: "sub"/"exp" via the
// standard JWT registered claims, plus "email" and "role" — see that
// file's `claims` struct.
export async function signDevToken(
  subject: string,
  email: string,
  role: 'admin' | 'developer',
): Promise<string> {
  const header = { alg: 'HS256', typ: 'JWT' }
  const payload = {
    sub: subject,
    email,
    role,
    exp: Math.floor(Date.now() / 1000) + 60 * 60 * 12, // 12 saat
  }
  const unsigned = `${base64urlFromString(JSON.stringify(header))}.${base64urlFromString(JSON.stringify(payload))}`

  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(DEV_JWT_SECRET),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  const signature = await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(unsigned))
  return `${unsigned}.${base64url(new Uint8Array(signature))}`
}
