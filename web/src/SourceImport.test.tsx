import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SourceImport } from './SourceImport'
import type { Document, Job, Me, Source, Statement, Unit } from './api'

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

const ДОКУМЕНТ: Document = {
  id: 5,
  sourceId: 1,
  filename: 'prikaz.docx',
  mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  byteSize: 40960,
  sha256: 'ab',
  uploadedBy: 'редактор',
}

const РАЗБОР: { units: Unit[]; statements: Statement[] } = {
  units: [
    {
      label: '3',
      parentLabel: '',
      title: 'Порядок',
      path: '3',
      depth: 0,
      kind: 'group',
      answerable: false,
      ord: 0,
    },
    {
      label: '3.2',
      parentLabel: '3',
      title: 'Сроки расмотрения',
      path: '3/3.2',
      depth: 1,
      kind: 'entry',
      answerable: true,
      ord: 1,
    },
    {
      label: '3.3',
      parentLabel: '3',
      title: 'Отказ',
      path: '3/3.3',
      depth: 1,
      kind: 'entry',
      answerable: false,
      ord: 2,
    },
  ],
  statements: [
    {
      unitLabel: '3.2',
      kind: 'rule',
      designation: 'абз. 1',
      body: 'Заявление рассматривается в десятидневный срок.',
      placeRef: 'с. 4',
      ord: 0,
    },
  ],
}

function job(over: Partial<Job> = {}): Job {
  return {
    id: 9,
    kind: 'parse',
    sourceId: 1,
    unitLabel: '',
    status: 'running',
    step: 'parse',
    stepWord: 'часть 12 из 80',
    attempts: 0,
    error: '',
    notes: [],
    createdAt: '2026-09-20T10:00:00Z',
    updatedAt: '2026-09-20T10:00:00Z',
    taskKind: '',
    unitTitle: '',
    unitWord: 'пункт',
    documentName: 'prikaz.docx',
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

const РЕДАКТОР: Me = {
  login: 'редактор',
  displayName: 'Редактор',
  permissions: ['source:read', 'source:accept'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['source:read'] }
const ГЕНЕРАТОР: Me = {
  login: 'генератор',
  displayName: 'Генератор',
  permissions: ['source:read', 'source:accept', 'generate'],
}

const ОБЫЧНО = {
  '/admin/api/sources/1/jobs': { jobs: [] },
  '/admin/api/sources/1/documents': { documents: [ДОКУМЕНТ] },
  '/admin/api/documents/5/draft': РАЗБОР,
  '/admin/api/sources/1': SOURCE,
}

describe('страница ввоза', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/sources/1/import')
    serve(ОБЫЧНО)
  })

  it('называет источник, в который ввозят', async () => {
    // В адресе стоит номер, и страница ввоза открывается прямой ссылкой
    // назавтра: не назови она источник — документ уехал бы в соседний.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Приказ № 1130н/)).toBeTruthy())
  })

  it('не обещает принять PDF позже', async () => {
    // Обещание, которое никто не собирается исполнять, отправляет ждать
    // вместо того, чтобы перевести файл.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/PDF переводит в текст/)).toBeTruthy())
    expect(screen.queryByText(/PDF.*позже|позже.*PDF/)).toBeNull()
  })

  it('человеку без права приёмки действия не показывает и говорит, почему', async () => {
    render(<SourceImport me={ЧИТАТЕЛЬ} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/кому выдано право принимать/)).toBeTruthy())
    expect(screen.queryByText('Посмотреть разбор')).toBeNull()
  })

  it('человеку без права генерации разбор моделью не предлагает и говорит, почему', async () => {
    // Документ при этом в списке есть: проверка на пустом списке прошла
    // бы и без права — показывать было бы нечего в любом случае.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('prikaz.docx')).toBeTruthy())
    expect(screen.queryByText('Разобрать моделью')).toBeNull()
    expect(screen.getByText(/право запускать\s+генерацию/)).toBeTruthy()
  })

  it('отдаёт документ модели и не ждёт разбора на открытой вкладке', async () => {
    // Документ на сотню страниц разбирается минутами: держать на нём
    // вкладку нельзя, и закрытая вкладка не отменяет оплаченного.
    const calls = serve(ОБЫЧНО)
    render(<SourceImport me={ГЕНЕРАТОР} id={1} onTitle={() => {}} />)

    fireEvent.click(await screen.findByText('Разобрать моделью'))
    await waitFor(() => expect(screen.getByText(/Документ отдан модели/)).toBeTruthy())
    expect(calls.some((one) => one === 'POST /admin/api/documents/5/parse')).toBe(true)
  })

  it('ход разбора показан словами, а не состоянием базы', async () => {
    serve({ ...ОБЫЧНО, '/admin/api/sources/1/jobs': { jobs: [job()] } })
    render(<SourceImport me={ГЕНЕРАТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('идёт: часть 12 из 80')).toBeTruthy())
  })

  it('замечания о сделанном показаны отдельно от отказа', async () => {
    // Задание, где одна часть из восьмидесяти не далась модели, — это
    // сделанная работа с потерей, а не провал; свали их в одно, и разбор
    // читался бы как неудавшийся.
    serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': {
        jobs: [job({ status: 'done', notes: ['часть 12 не разобрана'] })],
      },
    })
    render(<SourceImport me={ГЕНЕРАТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/часть 12 не разобрана/)).toBeTruthy())
    expect(screen.getByText('разобран')).toBeTruthy()
  })

  it('не опрашивает очередь, когда в ней нечего ждать', async () => {
    // Опрос «на всякий случай» на открытой сутками вкладке даёт тысячи
    // обращений в никуда, и замечают это по счёту за трафик.
    vi.useFakeTimers()
    try {
      const calls = serve({
        ...ОБЫЧНО,
        '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      })
      render(<SourceImport me={ГЕНЕРАТОР} id={1} onTitle={() => {}} />)
      await act(() => vi.advanceTimersByTimeAsync(100))
      const было = calls.filter((one) => one.includes('/jobs')).length

      await act(() => vi.advanceTimersByTimeAsync(10_000))
      expect(calls.filter((one) => one.includes('/jobs')).length).toBe(было)
    } finally {
      vi.useRealTimers()
    }
  })

  it('разбор показывается до принятия, а не после', async () => {
    // Принять разбор, не посмотрев, значит согласиться с чужим чтением
    // документа не читая: принятое становится истиной источника.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))

    await waitFor(() => expect(screen.getByText(/Вычитано единиц: 3/)).toBeTruthy())
    expect(screen.getByText('Принять разбор')).toBeTruthy()
  })

  it('правка разбора уходит на сервер целиком', async () => {
    // Черновик — один ответ модели на один документ, и склейка двух
    // ответов даёт разбор, которого не делал никто.
    const calls = serve(ОБЫЧНО)
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))

    const название = await screen.findByLabelText('Название 3.2')
    fireEvent.change(название, { target: { value: 'Сроки рассмотрения' } })
    fireEvent.click(screen.getByText('Записать правку'))

    await waitFor(() =>
      expect(calls.some((one) => one === 'PUT /admin/api/documents/5/draft')).toBe(true),
    )
  })

  it('принять, не записав правку, нельзя', async () => {
    // Принимает сервер то, что лежит у него, а не то, что видно на
    // экране: прими мы, не записав, — составитель увидел бы свою правку
    // и принятым чужой разбор, и разошлись бы они молча.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))

    const принять = await screen.findByText('Принять разбор')
    expect((принять as HTMLButtonElement).disabled).toBe(false)

    fireEvent.change(screen.getByLabelText('Название 3.2'), {
      target: { value: 'Сроки рассмотрения' },
    })
    expect((принять as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/Правка пока только на экране/)).toBeTruthy()
  })

  it('метка правке не отдаётся', async () => {
    // Меткой положение держится за свою единицу: переименуй её здесь — и
    // положения остались бы висеть на метке, которой больше нет, молча.
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))

    await waitFor(() => expect(screen.getByText('3.2')).toBeTruthy())
    expect(screen.queryByLabelText('Метка 3.2')).toBeNull()
  })

  it('единица без положений названа до заказа задачи, а не отказом в нём', async () => {
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))
    await waitFor(() => expect(screen.getByText(/положений нет/)).toBeTruthy())
  })

  it('пустой разбор говорит, что принимать нечего', async () => {
    serve({ ...ОБЫЧНО, '/admin/api/documents/5/draft': { units: [], statements: [] } })
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))
    await waitFor(() => expect(screen.getByText(/не вычиталось ни одной единицы/)).toBeTruthy())
  })

  it('разбор без списков не роняет страницу', async () => {
    // Список без списка — пустой список, а не падение раздела.
    serve({ ...ОБЫЧНО, '/admin/api/documents/5/draft': {} })
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Посмотреть разбор'))
    await waitFor(() => expect(screen.getByText(/не вычиталось ни одной единицы/)).toBeTruthy())
  })

  it('обратно к источнику есть дверь', async () => {
    render(<SourceImport me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('К источнику'))
    expect(window.location.pathname).toBe('/sources/1')
  })
})
