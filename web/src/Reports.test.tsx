import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Reports } from './Reports'
import type { CaseStats, Me } from './api'

const СМОТРИТЕЛЬ: Me = {
  login: 'смотритель',
  displayName: 'Смотритель',
  permissions: ['analytics'],
}
const РЕДАКТОР: Me = { login: 'редактор', displayName: 'Редактор', permissions: ['case:read'] }

function сводка(id: string, attempts: number, rate: number): CaseStats {
  return {
    caseId: id,
    attempts,
    correct: Math.round(attempts * rate),
    solveRate: rate,
    medianMs: 42000,
    confusion: {},
    origin: 'generated',
    enough: attempts >= 40,
  }
}

const ОТЧЁТ = {
  floor: 40,
  easy: [сводка('c-легко', 120, 0.98)],
  hard: [сводка('c-трудно', 3, 0.0)],
}

const СОБЫТИЯ = {
  days: 7,
  events: [{ name: 'case_opened', title: 'Задача открыта', count: 310, accounts: 12 }],
}

function serve(cases: unknown, events: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(path.startsWith('/admin/api/reports/cases') ? cases : events),
          { status: 200 },
        ),
      ),
    ),
  )
}

describe('отчёты', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve(ОТЧЁТ, СОБЫТИЯ)
  })

  it('доля по трём попыткам помечена ненадёжной', async () => {
    // Без метки составитель прочтёт «0 % верных» как приговор задаче, хотя
    // это три человека, а не свойство задачи.
    render(<Reports me={СМОТРИТЕЛЬ} />)
    await screen.findByText('c-трудно')
    expect(screen.getAllByText('попыток мало')).toHaveLength(1)
  })

  it('надёжной доле метки не ставит', async () => {
    render(<Reports me={СМОТРИТЕЛЬ} />)
    await screen.findByText('c-легко')
    const строка = screen.getByText('c-легко').closest('.list-row')
    expect(строка?.textContent).not.toContain('попыток мало')
    expect(строка?.textContent).toContain('98 %')
  })

  it('неразрешимая задача подсказывает искать разметку, а не сложность', async () => {
    // Задача, у которой верным помечен не тот вариант, выглядит ровно как
    // слишком трудная, и искать причину идут не туда.
    render(<Reports me={СМОТРИТЕЛЬ} />)
    expect(await screen.findByText(/проверьте разметку/i)).toBeTruthy()
  })

  it('число событий подписано, а не стоит голым рядом с числом врачей', async () => {
    // «310 · 12 врачей» не говорит, что первое число — разы: составитель
    // прочтёт его как что угодно.
    render(<Reports me={СМОТРИТЕЛЬ} />)
    const строка = (await screen.findByText('Задача открыта')).closest('.list-row')
    expect(строка?.textContent).toContain('310 всего')
    expect(строка?.textContent).toContain('12 врачей')
  })

  it('пустая воронка объясняет оба возможных объяснения', async () => {
    // «Событий нет» читается как «врачи ничего не делали», хотя вторая
    // причина — телеметрия, не доезжающая до сервера, — куда хуже.
    serve({ floor: 40, easy: [], hard: [] }, { days: 7, events: [] })
    render(<Reports me={СМОТРИТЕЛЬ} />)
    expect(await screen.findByText(/телеметрия до сервера не доезжает/)).toBeTruthy()
  })

  it('срок переключается, и сервер спрашивается заново', async () => {
    const asked: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        asked.push(path)
        return Promise.resolve(
          new Response(
            JSON.stringify(path.startsWith('/admin/api/reports/cases') ? ОТЧЁТ : СОБЫТИЯ),
            { status: 200 },
          ),
        )
      }),
    )
    render(<Reports me={СМОТРИТЕЛЬ} />)
    await screen.findByText('Задача открыта')
    fireEvent.click(screen.getByText('30 дней'))
    await waitFor(() => expect(asked.some((p) => p.includes('days=30'))).toBe(true))
  })

  it('без права отчётов раздел открыт и говорит, кому он выдан', async () => {
    // Раздел закрыт правом на сервере: спрятанная вкладка — подсказка, где
    // искать, а не запрет.
    serve(ОТЧЁТ, СОБЫТИЯ)
    render(<Reports me={РЕДАКТОР} />)
    expect(await screen.findByText(/кому выдано право «отчёты»/)).toBeTruthy()
  })
})
