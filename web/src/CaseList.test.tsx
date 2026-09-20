import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CaseList } from './CaseList'
import type { Case, Me, Source } from './api'

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

function задача(over: Partial<Case> = {}): Case {
  return {
    id: 'c-abcdefgh23456789',
    sourceId: 1,
    unitLabel: '3.1',
    unitPath: '3/3.1',
    status: 'draft',
    statusWord: 'черновик',
    revision: 1,
    origin: 'generated:составитель',
    body: {
      title: 'Срок рассмотрения',
      kind: 'recognise',
      segments: [{ text: 'Заявление подано в понедельник.', statements: ['абз. 1'] }],
      options: [
        { label: '3.1', text: 'Десять рабочих дней' },
        { label: '3.2', text: 'Тридцать календарных дней' },
      ],
      answer: '3.1',
      explanationMd: 'Срок назван прямо в положении.',
      difficulty: 2,
    },
    createdAt: '2026-09-19T10:00:00Z',
    updatedAt: '2026-09-19T10:00:00Z',
    ...over,
  }
}

function serve(answers: Record<string, unknown>) {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      calls.push(path)
      const key = Object.keys(answers).find((k) => path.startsWith(k))
      return Promise.resolve(new Response(JSON.stringify(key ? answers[key] : {}), { status: 200 }))
    }),
  )
  return calls
}

const СОСТАВИТЕЛЬ: Me = {
  login: 'составитель',
  displayName: 'Составитель',
  permissions: ['case:read', 'case:write', 'generate'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['case:read'] }

describe('список задач', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/cases')
  })

  it('показывает состояние словами составителя, а не кодом', async () => {
    serve({
      '/admin/api/sources': { sources: [SOURCE] },
      '/admin/api/cases': { cases: [задача({ status: 'published', statusWord: 'раздаётся' })] },
    })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)
    await waitFor(() => expect(screen.getByText('раздаётся')).toBeTruthy())
  })

  it('числа на вкладках берутся у сервера, а не считаются по показанному', async () => {
    // В списке лежат первые пятьдесят задач, и «Черновики 3» по ним
    // означало бы «три из показанных пятидесяти» — число, которое
    // меняется от прокрутки.
    serve({
      '/admin/api/sources': { sources: [SOURCE] },
      '/admin/api/cases': {
        cases: [задача()],
        counts: { all: 928, draft: 2, review: 0, published: 926, archived: 0 },
      },
    })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)

    const всё = await screen.findByRole('button', { name: /Всё/ })
    expect(всё.textContent).toContain('928')
    expect(screen.getByRole('button', { name: /Раздаются/ }).textContent).toContain('926')
  })

  it('молчит о числах, пока сервер их не прислал', async () => {
    // Ноль до ответа читается как «пусто», и составитель уходит из
    // раздела, не дождавшись списка.
    serve({
      '/admin/api/sources': { sources: [SOURCE] },
      '/admin/api/cases': { cases: [задача()] },
    })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)

    const всё = await screen.findByRole('button', { name: /Всё/ })
    expect(всё.textContent).not.toMatch(/\d/)
  })

  it('выбранная вкладка уходит в адрес, а не в состояние экрана', async () => {
    // «Покажи, что ты видишь» решается ссылкой, а «назад» браузера
    // возвращает к прежнему отбору.
    serve({
      '/admin/api/sources': { sources: [SOURCE] },
      '/admin/api/cases': { cases: [задача()], counts: { all: 1, draft: 1, review: 0, published: 0, archived: 0 } },
    })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)

    fireEvent.click(await screen.findByRole('button', { name: /Черновики/ }))
    expect(window.location.pathname + window.location.search).toBe('/cases?status=draft')
  })

  it('отбор по источнику и срезу уходит на сервер', async () => {
    const calls = serve({
      '/admin/api/sources': { sources: [SOURCE] },
      '/admin/api/cases': { cases: [задача()] },
    })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{ source: 1, path: '3', status: 'draft' }} />)

    await waitFor(() => expect(screen.getByText('Срок рассмотрения')).toBeTruthy())
    const запрос = calls.find((one) => one.startsWith('/admin/api/cases'))
    expect(запрос).toContain('source=1')
    expect(запрос).toContain('path=3')
    expect(запрос).toContain('status=draft')
  })

  it('набранный поиск не шлёт запрос на каждую клавишу', async () => {
    // Без задержки каждое нажатие — это шаг в истории браузера и запрос к
    // серверу: «F31.2» дало бы шесть шагов назад и шесть запросов, из
    // которых пять заказаны за то, чего составитель уже не ищет.
    vi.useFakeTimers()
    try {
      serve({
        '/admin/api/sources': { sources: [SOURCE] },
        '/admin/api/cases': { cases: [задача()] },
      })
      render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)
      await act(() => vi.advanceTimersByTimeAsync(400))

      const поле = screen.getByPlaceholderText(/название/)
      for (const value of ['F', 'F3', 'F31', 'F31.', 'F31.2']) {
        fireEvent.change(поле, { target: { value } })
        await act(() => vi.advanceTimersByTimeAsync(50))
      }
      expect(window.location.search).toBe('')

      // А остановившись — уходит, и один раз.
      await act(() => vi.advanceTimersByTimeAsync(400))
      expect(window.location.search).toBe('?q=F31.2')
    } finally {
      vi.useRealTimers()
    }
  })

  it('пустой ответ объясняет, почему пусто именно здесь', async () => {
    // Одно «Задач нет» на все случаи отправляет человека искать поломку:
    // отбор он ставил сам, а вот что отбор ничего не нашёл — это и есть
    // ответ.
    serve({ '/admin/api/sources': { sources: [SOURCE] }, '/admin/api/cases': { cases: [] } })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{ q: 'сроки' }} />)
    await waitFor(() => expect(screen.getByText(/По запросу «сроки»/)).toBeTruthy())
  })

  it('список, не прочитавший своё, не роняет раздел', async () => {
    // Список без списка — пустой список, а не падение: раздел, не
    // сумевший прочитать своё, обязан молчать в своих границах.
    serve({ '/admin/api/sources': { sources: [SOURCE] } })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Задачи' })).toBeTruthy())
  })

  it('человеку без права на генерацию заказ не предлагается', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел
    // — подсказка, где искать, а не запрет. Прячется только действие,
    // которое всё равно отказало бы.
    serve({ '/admin/api/sources': { sources: [SOURCE] }, '/admin/api/cases': { cases: [задача()] } })
    render(<CaseList me={ЧИТАТЕЛЬ} query={{}} />)

    await waitFor(() => expect(screen.getByText('Срок рассмотрения')).toBeTruthy())
    expect(screen.queryByRole('button', { name: 'Создать задачу' })).toBeNull()
  })

  it('сложность читается с экрана словами, а не четырьмя кружками', async () => {
    // Четыре кружка по шесть пикселей в дерево доступности не попадают:
    // без подписи читающему с экрана достаётся заголовок столбца и
    // ничего больше.
    serve({ '/admin/api/sources': { sources: [SOURCE] }, '/admin/api/cases': { cases: [задача()] } })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)
    await waitFor(() => expect(screen.getByLabelText('сложность 2 из 4')).toBeTruthy())
  })

  it('название задачи ведёт на её страницу', async () => {
    serve({ '/admin/api/sources': { sources: [SOURCE] }, '/admin/api/cases': { cases: [задача()] } })
    render(<CaseList me={СОСТАВИТЕЛЬ} query={{}} />)

    fireEvent.click(await screen.findByText('Срок рассмотрения'))
    expect(window.location.pathname).toBe('/cases/c-abcdefgh23456789')
  })
})
