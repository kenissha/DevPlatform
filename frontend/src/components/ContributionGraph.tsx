import { useMemo } from 'react'
import type { DayCount } from '../api/types'

// A year of commits as a week-per-column grid — the shape a contribution
// graph has settled into, read left-to-right as time and top-to-bottom
// as day of week.
//
// The backend hands over a continuous day range ending today (gaps
// included, see gitstats.Contributions), so this only has to pad the
// leading partial week to put every square under the right weekday.

const WEEKDAY_LABELS = ['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz']
const MONTH_LABELS = ['Oca', 'Şub', 'Mar', 'Nis', 'May', 'Haz', 'Tem', 'Ağu', 'Eyl', 'Eki', 'Kas', 'Ara']

// Monday-first, matching how the Turkish week is read — JS getDay() is
// Sunday-first, so Sunday (0) moves to the end.
function weekdayIndex(date: Date): number {
  return (date.getUTCDay() + 6) % 7
}

// Five buckets, like the familiar graph: none, then four intensities.
// Scaled against the busiest day rather than fixed thresholds, so a
// quiet repo still shows contrast instead of a uniformly pale grid.
function level(count: number, busiest: number): number {
  if (count <= 0) return 0
  if (busiest <= 1) return 4
  const ratio = count / busiest
  if (ratio > 0.66) return 4
  if (ratio > 0.33) return 3
  if (ratio > 0.1) return 2
  return 1
}

type Cell = { key: string; date: Date; count: number } | null

export function ContributionGraph({ days }: { days: DayCount[] }) {
  const { weeks, busiest, monthMarks } = useMemo(() => {
    const parsed = days.map((d) => ({
      key: d.date,
      // Parsed as UTC (the backend keys days in UTC): a bare
      // "YYYY-MM-DD" is already treated as UTC by Date, but building it
      // explicitly keeps that from depending on the reader knowing so.
      date: new Date(`${d.date}T00:00:00Z`),
      count: d.commits,
    }))

    const cells: Cell[] = []
    if (parsed.length > 0) {
      // Pad so the first real day lands on its own weekday row; the
      // padding cells render as empty space, not as zero-commit days.
      for (let i = 0; i < weekdayIndex(parsed[0].date); i++) cells.push(null)
    }
    cells.push(...parsed)

    const grouped: Cell[][] = []
    for (let i = 0; i < cells.length; i += 7) grouped.push(cells.slice(i, i + 7))

    // A month label sits above the first week that contains a day of
    // that month — the same anchoring the familiar graph uses, which
    // keeps labels from bunching up on short months.
    const marks: { index: number; label: string }[] = []
    let lastMonth = -1
    grouped.forEach((week, index) => {
      const first = week.find((c): c is NonNullable<Cell> => c !== null)
      if (!first) return
      const month = first.date.getUTCMonth()
      if (month !== lastMonth) {
        marks.push({ index, label: MONTH_LABELS[month] })
        lastMonth = month
      }
    })

    return {
      weeks: grouped,
      busiest: parsed.reduce((max, d) => Math.max(max, d.count), 0),
      monthMarks: marks,
    }
  }, [days])

  return (
    <div className="contrib">
      <div className="contrib-scroll">
        <div className="contrib-months">
          {monthMarks.map((m) => (
            // gridColumnStart is 1-based, and column 1 is the weekday
            // label gutter, so a week at index i sits in column i + 2.
            <span key={`${m.label}-${m.index}`} style={{ gridColumnStart: m.index + 2 }}>
              {m.label}
            </span>
          ))}
        </div>
        <div className="contrib-grid">
          <div className="contrib-weekdays">
            {WEEKDAY_LABELS.map((label, i) => (
              // Only every other label is drawn — seven labels at this
              // square size overlap into noise.
              <span key={label}>{i % 2 === 1 ? label : ''}</span>
            ))}
          </div>
          {weeks.map((week, wi) => (
            <div key={wi} className="contrib-week">
              {week.map((cell, di) =>
                cell === null ? (
                  <span key={di} className="contrib-cell is-pad" />
                ) : (
                  <span
                    key={cell.key}
                    className={`contrib-cell lvl-${level(cell.count, busiest)}`}
                    title={`${cell.date.toLocaleDateString('tr-TR', {
                      day: 'numeric',
                      month: 'long',
                      timeZone: 'UTC',
                    })} · ${cell.count} commit`}
                  />
                ),
              )}
            </div>
          ))}
        </div>
      </div>
      <div className="contrib-legend">
        <span>Az</span>
        {[0, 1, 2, 3, 4].map((l) => (
          <span key={l} className={`contrib-cell lvl-${l}`} />
        ))}
        <span>Çok</span>
      </div>
    </div>
  )
}
