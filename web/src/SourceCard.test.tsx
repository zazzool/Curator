import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SourceCard } from './SourceCard'
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

const ОБЫЧНО = {
  '/admin/api/sources/1/units': { units: UNITS },
  '/admin/api/sources/1/documents': { documents: [] },
  '/admin/api/sources/1': SOURCE,
}

describe('карточка источника', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/sources/1')
    serve(ОБЫЧНО)
  })

  it('зовёт единицы словом источника, а не «единицами»', async () => {
    // Слово выбирает составитель при заведении источника, и показать
    // вместо него «единицу» значит заговорить с ним на языке базы.
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Принятые пункты')).toBeTruthy())
  })

  it('показывает паспорт словами, а не значениями словаря', async () => {
    // По оси решается, складываются ли два источника в один список, по
    // полноте считаются доли охвата. Показать здесь «legal» и «fragment»
    // значит оставить объявление источника тайной базы.
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('приказ')).toBeTruthy())
    expect(screen.getByText('по правовым нормам')).toBeTruthy()
    expect(screen.getByText('разобранный кусок')).toBeTruthy()
  })

  it('краткое имя правке не отдаётся, и сказано почему до попытки', async () => {
    // По краткому имени приложение адресует справочник
    // (/v1/reference/sources/{slug}). Переименуй его — и у всех, кто уже
    // скачал источник, он пропадёт: сборка на руках обновится не завтра.
    // Сказано это в паспорте, а не отказом при записи: узнать надо ДО
    // того, как составитель соберётся править.
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/не правится/)).toBeTruthy())

    fireEvent.click(screen.getByText('Править'))
    // Поля нет вовсе, а не принято и отброшено: поле, которого сервер не
    // слушает, обещает правку, которой не будет.
    expect(screen.queryByText('Краткое имя')).toBeNull()
  })

  it('правка паспорта уходит на сервер и показывается сразу', async () => {
    const calls = serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1': { ...SOURCE, title: 'Приказ № 1130н (ред. 2026)' },
    })
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Править'))

    fireEvent.change(screen.getByLabelText('Название'), {
      target: { value: 'Приказ № 1130н (ред. 2026)' },
    })
    fireEvent.click(screen.getByText('Записать паспорт'))

    await waitFor(() => expect(screen.getByText('Паспорт источника записан.')).toBeTruthy())
    expect(calls.some((one) => one === 'PUT /admin/api/sources/1')).toBe(true)
  })

  it('человеку без права приёмки паспорт править нечем', async () => {
    // Раздел при этом виден: право проверяет сервер, а скрытый раздел —
    // подсказка, где искать, а не запрет.
    render(<SourceCard me={ЧИТАТЕЛЬ} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Паспорт')).toBeTruthy())
    expect(screen.queryByText('Править')).toBeNull()
  })

  it('показывает принятое деревом по глубине пути', async () => {
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    const вложенная = await screen.findByText('Сроки')
    expect(вложенная.closest('li')?.style.paddingLeft).toBe('16px')
  })

  it('раздел помечен, а запись нет', async () => {
    // По разделу не спрашивают, и составитель, не видя этого, ищет
    // пропавшие задачи в генерации, а не в разборе.
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('раздел')).toBeTruthy())
    expect(screen.getAllByText('раздел')).toHaveLength(1)
  })

  it('ввоз документов стоит дверью, а не разделом', async () => {
    // Ввоз — работа на полчаса с чужим документом перед глазами, и делить
    // экран с паспортом ей незачем. Здесь только дверь туда.
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    fireEvent.click(await screen.findByText('Принести документ'))
    expect(window.location.pathname).toBe('/sources/1/import')
  })

  it('дверь к задачам уносит источник и срез с собой', async () => {
    // Метка, переписанная руками из одного списка в другой, ошибается.
    vi.useFakeTimers()
    try {
      serve(ОБЫЧНО)
      render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
      await act(() => vi.advanceTimersByTimeAsync(10))

      fireEvent.change(screen.getByLabelText('Срез по пути'), { target: { value: '3' } })
      await act(() => vi.advanceTimersByTimeAsync(400))
      fireEvent.click(screen.getByText('Показать задачи'))

      expect(window.location.pathname + window.location.search).toBe('/cases?source=1&path=3')
    } finally {
      vi.useRealTimers()
    }
  })

  it('прочитанное название уходит наверх, в полосу', async () => {
    // В адресе стоит только номер: пришедший по прямой ссылке иначе видел
    // бы полосу без названия до самого ухода с экрана.
    const наверх = vi.fn()
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={наверх} />)
    await waitFor(() => expect(наверх).toHaveBeenCalledWith('Приказ № 1130н'))
  })

  it('черновик говорит, что для приложения его не существует', async () => {
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/для приложения этого источника/)).toBeTruthy())
  })

  it('действующий источник предлагает снять, а не объявить заново', async () => {
    serve({ ...ОБЫЧНО, '/admin/api/sources/1': { ...SOURCE, status: 'active' } })
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Снять с раздачи')).toBeTruthy())
    expect(screen.queryByText('Объявить действующим')).toBeNull()
  })

  it('человеку без права приёмки состояние менять нечем', async () => {
    render(<SourceCard me={ЧИТАТЕЛЬ} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Состояние')).toBeTruthy())
    expect(screen.queryByText('Объявить действующим')).toBeNull()
  })

  it('раздел, не прочитавший своё, не роняет остальной экран', async () => {
    // Ответ без поля documents ронял ВЕСЬ экран источника белым — вместе
    // с принятым, к документам отношения не имеющим.
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1/documents': {},
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Сроки')).toBeTruthy())
  })

  it('говорит числом, что показаны не все единицы', async () => {
    // «Показаны не все» без числа не даёт понять, насколько не все.
    serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/units': { units: UNITS, limit: 2, more: true },
    })
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Показаны первые 2/)).toBeTruthy())
  })

  it('о полном списке не говорит, что он обрезан', async () => {
    serve({ ...ОБЫЧНО, '/admin/api/sources/1/units': { units: UNITS, limit: 2, more: false } })
    render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
    await waitFor(() => expect(screen.getByText('Сроки')).toBeTruthy())
    expect(screen.queryByText(/Показаны первые/)).toBeNull()
  })

  it('набранный отбор не шлёт запрос на каждую клавишу', async () => {
    // «F31.2» — двадцать обращений за секунду, и все, кроме последнего,
    // заказаны за то, чего составитель уже не ищет.
    vi.useFakeTimers()
    try {
      const calls = serve(ОБЫЧНО)
      render(<SourceCard me={РЕДАКТОР} id={1} onTitle={() => {}} />)
      await act(() => vi.advanceTimersByTimeAsync(400))
      const было = calls.filter((one) => one.includes('/units')).length

      for (const буква of ['3', '3.', '3.2']) {
        fireEvent.change(screen.getByLabelText('Срез по пути'), { target: { value: буква } })
        await act(() => vi.advanceTimersByTimeAsync(50))
      }
      expect(calls.filter((one) => one.includes('/units')).length).toBe(было)

      await act(() => vi.advanceTimersByTimeAsync(400))
      expect(calls.filter((one) => one.includes('/units')).length).toBe(было + 1)
    } finally {
      vi.useRealTimers()
    }
  })
})
