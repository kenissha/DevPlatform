// Inline SVG icons. Kept local rather than pulled from an icon package:
// the set is small, and a strict-CSP self-contained build shouldn't grow a
// dependency for six glyphs. All are 16x16 on a `currentColor` stroke, so
// they inherit whatever text colour their context sets.

type IconProps = { className?: string }

// width/height are set as attributes, not left to CSS: an SVG with only a
// viewBox stretches to fill its container, so a caller that renders an icon
// somewhere without a matching size rule gets a full-width glyph. The
// attributes give every icon an intrinsic size; CSS still overrides where a
// context wants a different one.
const base = {
  viewBox: '0 0 16 16',
  width: 16,
  height: 16,
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

export function LogoMark({ className }: IconProps) {
  // Three nodes joined by a fork — a branch/merge motif, DevPlatform's own
  // mark rather than a borrowed one.
  return (
    <svg className={className} viewBox="0 0 24 24" width={24} height={24} fill="none" aria-hidden="true">
      <path
        d="M7 6.5v5a4 4 0 0 0 4 4h3"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
      <circle cx="7" cy="4" r="2.4" stroke="currentColor" strokeWidth="2" />
      <circle cx="7" cy="19.5" r="2.4" stroke="currentColor" strokeWidth="2" />
      <circle cx="17" cy="15.5" r="2.4" stroke="currentColor" strokeWidth="2" />
    </svg>
  )
}

export function RepoIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M3.5 2.5h9v11h-9z" />
      <path d="M3.5 11h9" />
      <path d="M6 2.5v8.5" />
    </svg>
  )
}

export function BranchIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M4.5 4.5v7" />
      <circle cx="4.5" cy="3" r="1.6" />
      <circle cx="4.5" cy="13" r="1.6" />
      <circle cx="11.5" cy="5" r="1.6" />
      <path d="M10 6.2c-.6 2-2.4 2.8-5.5 2.8" />
    </svg>
  )
}

export function TaskIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h7" />
    </svg>
  )
}

export function MergeIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="4" cy="3.5" r="1.6" />
      <circle cx="4" cy="12.5" r="1.6" />
      <circle cx="12" cy="8" r="1.6" />
      <path d="M4 5.1v5.8" />
      <path d="M5.6 5.6c1 1.6 2.6 2.4 4.8 2.4" />
    </svg>
  )
}

export function AuditIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="8" cy="8" r="5.5" />
      <path d="M8 5v3l2 1.5" />
    </svg>
  )
}

export function DeployIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M8 1.5c1.8 1.8 2.5 4 2.5 6.2 0 1.5-.5 2.8-1.2 3.8L8 13.5l-1.3-2c-.7-1-1.2-2.3-1.2-3.8 0-2.2.7-4.4 2.5-6.2Z" />
      <circle cx="8" cy="7" r="1.3" />
      <path d="M5.8 10.8 4 12.5v-2M10.2 10.8l1.8 1.7v-2" />
    </svg>
  )
}

export function ChartIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M2.5 13.5h11" />
      <path d="M4.5 13.5v-4M8 13.5v-8M11.5 13.5v-5.5" />
    </svg>
  )
}

export function BellIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M4 11.5V7a4 4 0 0 1 8 0v4.5l1.2 1.5H2.8z" />
      <path d="M6.5 13.5a1.6 1.6 0 0 0 3 0" />
    </svg>
  )
}

export function OverviewIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <rect x="2.5" y="2.5" width="5" height="5" rx="1" />
      <rect x="8.5" y="2.5" width="5" height="5" rx="1" />
      <rect x="2.5" y="8.5" width="5" height="5" rx="1" />
      <rect x="8.5" y="8.5" width="5" height="5" rx="1" />
    </svg>
  )
}

export function LockIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <rect x="3" y="7.5" width="10" height="6.5" rx="1.2" />
      <path d="M5 7.5V5a3 3 0 0 1 6 0v2.5" />
      <path d="M8 10v2" />
    </svg>
  )
}

export function KeyIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="5" cy="10" r="2.5" />
      <path d="M7 8.5L13 2.5" />
      <path d="M10.5 5L12.5 7" />
    </svg>
  )
}

export function SunIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="8" cy="8" r="3.1" />
      <path d="M8 1v1.6M8 13.4V15M15 8h-1.6M2.6 8H1M12.95 3.05l-1.13 1.13M4.18 11.82l-1.13 1.13M12.95 12.95l-1.13-1.13M4.18 4.18L3.05 3.05" />
    </svg>
  )
}

export function MoonIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M13.5 9.6A5.9 5.9 0 0 1 6.4 2.5a5.9 5.9 0 1 0 7.1 7.1z" />
    </svg>
  )
}

export function ClockIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="8" cy="8" r="6.4" />
      <path d="M8 4.4V8l2.4 1.6" />
    </svg>
  )
}

export function CheckIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M3 8.5l3.4 3.4L13 4.8" />
    </svg>
  )
}

export function PlusIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <path d="M8 3v10M3 8h10" />
    </svg>
  )
}
