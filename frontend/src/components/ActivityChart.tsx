import { useState } from 'react'
import type { DayCount } from '../api/types'

// Commit activity over time: one series, magnitude per day → a column
// chart in a single hue. No legend (one series — the card's title says what
// is plotted), no value on every column (the axis, the tooltip and the
// table view carry those). Both accent steps were checked against their
// surfaces for contrast before being used here.

const W = 720
const H = 168
const PAD_L = 30
const PAD_R = 6
const PAD_T = 12
const PAD_B = 24

const GAP = 2 // the surface gap that separates touching columns
const MAX_BAR_W = 24
const RADIUS = 4 // rounded data-end; the baseline end stays square

export function ActivityChart({ days }: { days: DayCount[] }) {
  const [hovered, setHovered] = useState<number | null>(null)

  if (days.length === 0) return null

  const plotW = W - PAD_L - PAD_R
  const plotH = H - PAD_T - PAD_B
  const band = plotW / days.length
  const barW = Math.min(MAX_BAR_W, Math.max(1, band - GAP))

  const max = Math.max(1, ...days.map((d) => d.commits))
  const baseline = PAD_T + plotH
  const total = days.reduce((sum, d) => sum + d.commits, 0)

  const bandX = (i: number) => PAD_L + i * band
  const barX = (i: number) => bandX(i) + (band - barW) / 2
  const barH = (count: number) => (count / max) * plotH

  const active = hovered !== null ? days[hovered] : null

  return (
    <div className="chart">
      <div className="chart-plot">
        <svg viewBox={`0 0 ${W} ${H}`} className="chart-svg" role="img"
             aria-label={`Son ${days.length} günde ${total} commit`}>
          {/* Recessive hairline axis + top gridline; solid, one step off surface. */}
          <line x1={PAD_L} y1={PAD_T} x2={W - PAD_R} y2={PAD_T} className="chart-grid" />
          <line x1={PAD_L} y1={baseline} x2={W - PAD_R} y2={baseline} className="chart-axis" />

          <text x={PAD_L - 8} y={PAD_T + 4} className="chart-tick" textAnchor="end">
            {max}
          </text>
          <text x={PAD_L - 8} y={baseline} className="chart-tick" textAnchor="end">
            0
          </text>

          {days.map((d, i) => {
            const h = barH(d.commits)
            if (h <= 0) return null
            return (
              <path
                key={d.date}
                d={columnPath(barX(i), baseline - h, barW, h, RADIUS)}
                className={`chart-bar${hovered === i ? ' is-hovered' : ''}`}
              />
            )
          })}

          {/* Hit targets: a full-height transparent band per day, so the
              pointer only has to be near the column, not on it. */}
          {days.map((d, i) => (
            <rect
              key={`hit-${d.date}`}
              x={bandX(i)}
              y={PAD_T}
              width={band}
              height={plotH}
              fill="transparent"
              tabIndex={0}
              role="button"
              aria-label={`${formatDay(d.date)}: ${d.commits} commit`}
              onMouseEnter={() => setHovered(i)}
              onMouseLeave={() => setHovered(null)}
              onFocus={() => setHovered(i)}
              onBlur={() => setHovered(null)}
            />
          ))}

          {/* A few date ticks only — one label per column would be unreadable. */}
          {tickIndexes(days.length).map((i) => (
            <text
              key={`tick-${i}`}
              x={bandX(i) + band / 2}
              y={H - 6}
              className="chart-tick"
              textAnchor="middle"
            >
              {formatDay(days[i].date)}
            </text>
          ))}
        </svg>

        {active && (
          <div
            className="chart-tooltip"
            style={{ left: `${((bandX(hovered!) + band / 2) / W) * 100}%` }}
          >
            <strong>{active.commits}</strong> commit
            <span className="muted"> · {formatDay(active.date)}</span>
          </div>
        )}
      </div>

      {/* Table view: every value the tooltip shows stays reachable without
          hovering, including for screen readers. */}
      <table className="visually-hidden">
        <caption>Günlük commit sayısı</caption>
        <thead>
          <tr>
            <th scope="col">Tarih</th>
            <th scope="col">Commit</th>
          </tr>
        </thead>
        <tbody>
          {days.map((d) => (
            <tr key={d.date}>
              <th scope="row">{d.date}</th>
              <td>{d.commits}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// A column with its data-end rounded and its baseline end square. Radius
// collapses on short columns so a 1-commit day doesn't render as a blob.
function columnPath(x: number, y: number, w: number, h: number, r: number): string {
  const radius = Math.min(r, w / 2, h)
  return [
    `M${x},${y + h}`,
    `L${x},${y + radius}`,
    `Q${x},${y} ${x + radius},${y}`,
    `L${x + w - radius},${y}`,
    `Q${x + w},${y} ${x + w},${y + radius}`,
    `L${x + w},${y + h}`,
    'Z',
  ].join(' ')
}

// Roughly six evenly spaced ticks, always including the last day.
function tickIndexes(count: number): number[] {
  if (count <= 6) return Array.from({ length: count }, (_, i) => i)
  const step = Math.ceil(count / 6)
  const out: number[] = []
  for (let i = count - 1; i >= 0; i -= step) out.unshift(i)
  return out
}

function formatDay(date: string): string {
  const d = new Date(date + 'T00:00:00Z')
  return d.toLocaleDateString('tr-TR', { day: 'numeric', month: 'short', timeZone: 'UTC' })
}
