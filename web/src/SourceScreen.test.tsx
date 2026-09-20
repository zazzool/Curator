import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SourceScreen } from './SourceScreen'
import type { Me, Source, Unit } from './api'

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
    title: 'Сроки',
    path: '3/3.2',
    depth: 1,
    kind: 'entry',
    answerable: true,
    ord: 1,
  },
]

function serve(answers: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      const key = Object.keys(answers).find((k) => path.startsWith(k))
      return Promise.resolve(
        new Response(JSON.stringify(key ? answers[key] : {}), { status: 200 }),
      )
    }),
  )
}

const РЕДАКТОР: Me = { login: 'редактор', displayName: 'Редактор', permissions: ['source:read', 'source:accept'] }
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['source:read'] }

describe('экран источника', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1/jobs': { jobs: [] },
      '/admin/api/cases': { cases: [] },
      '/admin/api/sources/1/documents': { documents: [] },
      '/admin/api/sources/1': SOURCE,
    })
  })

  it('зовёт единицы словом источника, а не «единицами»', async () => {
    // Интерфейс без словаря источника показал бы «единица» врачу, который
    // ждёт слова «пункт». Ради этого слово и хранится у источника.
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
  })

  it('показывает принятое деревом по глубине пути', async () => {
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText('3.2')).toBeTruthy())
    const nested = screen.getByText('3.2').closest('li')
    expect(nested?.style.paddingLeft).toBe('16px')
  })

  it('раздел, не прочитавший своё, не роняет остальной экран', async () => {
    // Обнаружилось проверкой, а не на бою: ответ без списка заданий ронял
    // весь экран источника белым — вместе с документами и принятым,
    // к генерации отношения не имеющими.
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1/documents': { documents: [] },
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
  })

  it('не обещает принять PDF позже', async () => {
    // Перевод PDF в текст делает служба снаружи — это выбранная граница, а
    // не очередь работ. Обещание «позже» отправляет человека ждать вместо
    // того, чтобы перевести файл.
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    const hint = await screen.findByText(/PDF/)
    expect(hint.textContent).not.toMatch(/позже|пока|скоро/)
  })

  it('разбор показывается до принятия, а не после', async () => {
    // Принятый разбор — решение о том, что теперь считается истиной
    // источника, и жалось оно вслепую: ни единиц, ни положений
    // составителю не показывалось, хотя ручки были написаны. Пояснение к
    // экрану при этом обещало «посмотреть куски».
    const ДОКУМЕНТ = {
      id: 5,
      sourceId: 1,
      filename: 'prikaz.docx',
      mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      byteSize: 40960,
      uploadedBy: 'редактор',
    }
    const принятые: number[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/accept')) {
          принятые.push(5)
          return Promise.resolve(new Response(JSON.stringify({ accepted: 2 }), { status: 200 }))
        }
        if (path.startsWith('/admin/api/documents/5/draft')) {
          return Promise.resolve(
            new Response(JSON.stringify({ units: UNITS, statements: [] }), { status: 200 }),
          )
        }
        if (path.startsWith('/admin/api/sources/1/documents')) {
          return Promise.resolve(
            new Response(JSON.stringify({ documents: [ДОКУМЕНТ] }), { status: 200 }),
          )
        }
        if (path.startsWith('/admin/api/sources/1/units')) {
          return Promise.resolve(new Response(JSON.stringify({ units: [] }), { status: 200 }))
        }
        return Promise.resolve(new Response(JSON.stringify(SOURCE), { status: 200 }))
      }),
    )
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)

    // Принять сразу нечем: сперва показ.
    expect(await screen.findByText('Посмотреть разбор')).toBeTruthy()
    expect(screen.queryByText('Принять разбор')).toBeNull()

    fireEvent.click(screen.getByText('Посмотреть разбор'))
    expect(await screen.findByText(/Вычитано единиц: 2/)).toBeTruthy()
    // Единица без положений названа прямо: по ней нельзя заказать задачу.
    expect(screen.getByText(/положений нет/)).toBeTruthy()
    expect(принятые).toEqual([])

    fireEvent.click(screen.getByText('Принять разбор'))
    await waitFor(() => expect(screen.getByText(/Разбор принят/)).toBeTruthy())
    expect(принятые).toEqual([5])
  })

  it('человеку без права приёмки не показывает действие и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    render(<SourceScreen me={ЧИТАТЕЛЬ} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
    expect(screen.queryByText('Принять разбор')).toBeNull()
    expect(screen.getByText(/кому выдано право принимать/)).toBeTruthy()
  })

  it('черновик говорит, что для приложения его не существует', async () => {
    // Состояния источника не было вовсе: ставилось умолчание «черновик», а
    // менять его было нечем — и справочник в приложении оставался пуст у
    // всех и всегда.
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await waitFor(() =>
      expect(screen.getByText(/для приложения этого источника не существует/i)).toBeTruthy(),
    )
    expect(screen.getByText('Объявить действующим')).toBeTruthy()
  })

  it('действующий источник предлагает снять, а не объявить заново', async () => {
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1/jobs': { jobs: [] },
      '/admin/api/cases': { cases: [] },
      '/admin/api/sources/1/documents': { documents: [] },
      '/admin/api/sources/1': { ...SOURCE, status: 'active' },
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText('Снять с раздачи')).toBeTruthy())
  })

  it('раздел помечен, а запись нет', async () => {
    // По разделу не спрашивают, и составитель, не видя этого, ищет
    // пропавшие задачи в генерации, а не в разборе. У записи род —
    // умолчание, и метка у каждой строки была бы шумом.
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    await screen.findByText('Порядок')
    expect(screen.getAllByText('раздел')).toHaveLength(1)
  })

  it('человеку без права приёмки состояние менять нечем', async () => {
    // Решить, что источник теперь учит врача, — то же решение, что принять
    // разбор, и право у них одно.
    render(<SourceScreen me={ЧИТАТЕЛЬ} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Состояние' })).toBeTruthy())
    expect(screen.queryByText('Объявить действующим')).toBeNull()
  })
})
