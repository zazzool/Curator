import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Generation } from './Generation'
import type { Job, Me, Source, Unit } from './api'

const SOURCE: Source = {
  id: 1,
  slug: 'prikaz-1130n',
  kind: 'decree',
  title: 'Приказ № 1130н',
  unitWord: 'пункт',
  statementWord: 'положение',
  purpose: 'legal',
  hierarchy: 'part-of',
  completeness: 'fragment',
  edition: '',
  status: 'draft',
}

const UNITS: Unit[] = [
  { label: '3', parentLabel: '', title: 'Порядок', path: '3', depth: 0, kind: 'entry', answerable: false, ord: 0 },
  { label: '3.1', parentLabel: '3', title: 'Сроки', path: '3/3.1', depth: 1, kind: 'entry', answerable: true, ord: 1 },
]

function job(over: Partial<Job> = {}): Job {
  return {
    id: 7,
    sourceId: 1,
    unitLabel: '3.1',
    status: 'queued',
    step: 'compose',
    stepWord: 'написание',
    attempts: 0,
    error: '',
    createdAt: '2026-09-19T10:00:00Z',
    updatedAt: '2026-09-19T10:00:00Z',
    taskKind: 'recognise',
    unitTitle: 'Сроки',
    unitWord: 'пункт',
    ...over,
  }
}

function serve(answers: Record<string, unknown>) {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${path}`)
      const key = Object.keys(answers).find((k) => path.startsWith(k))
      return Promise.resolve(new Response(JSON.stringify(key ? answers[key] : {}), { status: 200 }))
    }),
  )
  return calls
}

const СОСТАВИТЕЛЬ: Me = {
  login: 'составитель',
  displayName: 'Составитель',
  permissions: ['source:read', 'generate'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['source:read'] }

describe('генерация по источнику', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('заказывать предлагает только те единицы, по которым есть что спрашивать', async () => {
    // Группа отказала бы на сервере, и список, показывающий её, отправил бы
    // составителя получать отказ.
    serve({ '/admin/api/sources/1/jobs': { jobs: [] } })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('3.1 — Сроки')).toBeTruthy())
    expect(screen.queryByText('3 — Порядок')).toBeNull()
  })

  it('зовёт единицу словом источника', async () => {
    serve({ '/admin/api/sources/1/jobs': { jobs: [] } })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)
    await waitFor(() => expect(screen.getByText('Выберите пункт')).toBeTruthy())
  })

  it('человеку без права на генерацию не показывает заказ и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    serve({ '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] } })
    render(<Generation me={ЧИТАТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('Сроки')).toBeTruthy())
    expect(screen.queryByText('Заказать задачу')).toBeNull()
    expect(screen.getByText(/кому выдано право на генерацию/)).toBeTruthy()
  })

  it('показывает состояние словами и называет причину отказа прямо в строке', async () => {
    // «failed» составителю ничего не говорит, а причина, спрятанная за
    // нажатием, не читается никем.
    serve({
      '/admin/api/sources/1/jobs': {
        jobs: [job({ status: 'failed', error: 'у пункта 3.1 нет положений' })],
      },
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() =>
      expect(screen.getByText(/не вышло: у пункта 3.1 нет положений/)).toBeTruthy(),
    )
  })

  it('отказ сервера показывается как есть', async () => {
    // Сервер пишет отказ по-русски и говорит, чего не хватает. «Что-то
    // пошло не так» отправило бы составителя перебирать поля вслепую.
    vi.stubGlobal(
      'fetch',
      vi.fn((_path: string, init?: RequestInit) => {
        if ((init?.method ?? 'GET') === 'POST') {
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'По группе задачу не напишешь' }), {
              status: 400,
            }),
          )
        }
        return Promise.resolve(new Response(JSON.stringify({ jobs: [] }), { status: 200 }))
      }),
    )
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('3.1 — Сроки')).toBeTruthy())
    fireEvent.change(screen.getAllByRole('combobox')[0]!, { target: { value: '3.1' } })
    fireEvent.click(screen.getByText('Заказать задачу'))

    await waitFor(() => expect(screen.getByText('По группе задачу не напишешь')).toBeTruthy())
  })

  it('разметку показывает прямо в условии, а не сноской под ним', async () => {
    // Составитель проверяет именно её — какой фрагмент какое положение
    // подтверждает, — и сноска заставила бы его считать фрагменты глазами.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({
        status: 'done',
        drafts: [
          {
            title: 'Срок рассмотрения',
            segments: [
              { text: 'Заявление подано в понедельник.', statements: ['абз. 1'] },
              { text: 'Заявитель ждёт ответа.' },
            ],
            options: [
              { label: '3.1', text: 'Десять рабочих дней' },
              { label: '3.2', text: 'Отказ письменно' },
            ],
            answer: '3.1',
            explanationMd: 'Срок назван прямо в положении.',
            difficulty: 2,
          },
        ],
      }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    fireEvent.click(screen.getByText('Открыть'))

    const marked = await screen.findByText(/абз\. 1/)
    expect(marked.closest('p')?.textContent).toMatch(/Заявление подано в понедельник/)
    expect(screen.getByText(/заказанный ответ/)).toBeTruthy()
  })

  it('не опрашивает очередь, когда в ней нечего ждать', async () => {
    // Опрос «на всякий случай» на открытой сутками вкладке даёт тысячи
    // обращений в никуда, и замечают это по счёту за трафик.
    vi.useFakeTimers()
    const calls = serve({ '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] } })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await vi.waitFor(() => expect(calls.length).toBe(1))
    vi.advanceTimersByTime(30000)
    expect(calls.length).toBe(1)
    vi.useRealTimers()
  })
})
