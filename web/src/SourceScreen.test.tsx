import { render, screen, waitFor } from '@testing-library/react'
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
  { label: '3', parentLabel: '', title: 'Порядок', path: '3', depth: 0, answerable: true, ord: 0 },
  { label: '3.2', parentLabel: '3', title: 'Сроки', path: '3/3.2', depth: 1, answerable: true, ord: 1 },
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

  it('человеку без права приёмки не показывает действие и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    render(<SourceScreen me={ЧИТАТЕЛЬ} id={1} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
    expect(screen.queryByText('Принять разбор')).toBeNull()
    expect(screen.getByText(/кому выдано право принимать/)).toBeTruthy()
  })
})
