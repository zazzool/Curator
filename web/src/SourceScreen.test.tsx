import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
const ГЕНЕРАТОР: Me = {
  login: 'генератор',
  displayName: 'Генератор',
  permissions: ['source:read', 'source:accept', 'generate'],
}

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
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
  })

  it('показывает принятое деревом по глубине пути', async () => {
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText('3.2')).toBeTruthy())
    const nested = screen.getByText('3.2').closest('li')
    expect(nested?.style.paddingLeft).toBe('16px')
  })

  it('раздел, не прочитавший своё, не роняет остальной экран', async () => {
    // Обнаружилось проверкой, а не на бою: ответ без списка документов
    // ронял весь экран источника белым — вместе с принятым, к документам
    // отношения не имеющим. Раздел, не сумевший прочитать своё, обязан
    // молчать в своих границах.
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
  })

  it('дверь к задачам уносит источник и срез с собой', async () => {
    // Метка, переписанная руками из одного списка в другой, ошибается —
    // и ошибается молча: список задач по чужому срезу выглядит как
    // «задач нет».
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)

    const поле = await screen.findByPlaceholderText(/например/)
    fireEvent.change(поле, { target: { value: '3' } })
    await act(() => new Promise((done) => setTimeout(done, 400)))

    fireEvent.click(screen.getByText('Показать задачи'))
    expect(window.location.pathname + window.location.search).toBe('/cases?source=1&path=3')
  })

  it('прочитанное название уходит наверх, в полосу', async () => {
    // В адресе стоит только номер, и пришедший по прямой ссылке иначе
    // видел бы полосу без названия до самого ухода с экрана.
    const названия: string[] = []
    render(
      <SourceScreen
        me={РЕДАКТОР}
        id={1}
        onTitle={(title) => названия.push(title)}
        onBack={() => {}}
      />,
    )
    await waitFor(() => expect(названия).toContain('Приказ № 1130н'))
  })

  it('не обещает принять PDF позже', async () => {
    // Перевод PDF в текст делает служба снаружи — это выбранная граница, а
    // не очередь работ. Обещание «позже» отправляет человека ждать вместо
    // того, чтобы перевести файл.
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
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
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)

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

  it('отдаёт документ модели и не ждёт разбора на открытой вкладке', async () => {
    // Документ на сотню страниц разбирается минутами: заказ уходит в
    // очередь, а ответ приходит сразу. Делай ручка разбор на месте, закрытая
    // вкладка отменяла бы уже оплаченное.
    const ДОКУМЕНТ = {
      id: 5,
      sourceId: 1,
      filename: 'prikaz.docx',
      mime: 'text/plain',
      byteSize: 40960,
      uploadedBy: 'редактор',
    }
    const заказы: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/parse')) {
          заказы.push(path)
          return Promise.resolve(
            new Response(JSON.stringify({ id: 9, kind: 'parse', status: 'queued' }), {
              status: 201,
            }),
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
        if (path.startsWith('/admin/api/sources/1/jobs')) {
          return Promise.resolve(new Response(JSON.stringify({ jobs: [] }), { status: 200 }))
        }
        if (path.startsWith('/admin/api/cases')) {
          return Promise.resolve(new Response(JSON.stringify({ cases: [] }), { status: 200 }))
        }
        return Promise.resolve(new Response(JSON.stringify(SOURCE), { status: 200 }))
      }),
    )
    render(<SourceScreen me={ГЕНЕРАТОР} id={1} onBack={() => {}} />)

    fireEvent.click(await screen.findByText('Разобрать моделью'))
    await waitFor(() => expect(заказы).toEqual(['/admin/api/documents/5/parse']))
    // Сказано и то, что разбор идёт не мгновенно, и то, что принимать его
    // придётся отдельно: молчание здесь читается как «источник пополнен».
    const note = await screen.findByText(/Документ отдан модели/)
    expect(note.textContent).toMatch(/принять его надо будет отдельно/)
  })

  it('отказ разбора показывается словами сервера, а не общей неудачей', async () => {
    // «Этот документ уже разбирается» и «разбор не заказан» требуют разных
    // действий: подождать и позвать снова. Общий текст отправляет жать
    // кнопку по второму разу там, где это заведёт второй разбор.
    const ДОКУМЕНТ = {
      id: 5,
      sourceId: 1,
      filename: 'prikaz.docx',
      mime: 'text/plain',
      byteSize: 40960,
      uploadedBy: 'редактор',
    }
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/parse')) {
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'Этот документ уже разбирается' }), {
              status: 409,
            }),
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
        if (path.startsWith('/admin/api/sources/1/jobs')) {
          return Promise.resolve(new Response(JSON.stringify({ jobs: [] }), { status: 200 }))
        }
        if (path.startsWith('/admin/api/cases')) {
          return Promise.resolve(new Response(JSON.stringify({ cases: [] }), { status: 200 }))
        }
        return Promise.resolve(new Response(JSON.stringify(SOURCE), { status: 200 }))
      }),
    )
    render(<SourceScreen me={ГЕНЕРАТОР} id={1} onBack={() => {}} />)
    fireEvent.click(await screen.findByText('Разобрать моделью'))
    expect(await screen.findByText(/уже разбирается/)).toBeTruthy()
  })

  it('человеку без права генерации не показывает разбор моделью и говорит, почему', async () => {
    // У права приёмки и права генерации разные предметы: принять разбор —
    // решить, что теперь истина источника; заказать разбор — потратить
    // деньги у поставщика моделей. Редактор с первым правом, но без
    // второго, кнопки не видит.
    serve({
      '/admin/api/sources/1/units': { units: UNITS },
      '/admin/api/sources/1/jobs': { jobs: [] },
      '/admin/api/cases': { cases: [] },
      // Документ в источнике есть: проверка, сделанная на пустом списке,
      // подтверждала бы отсутствие кнопки там, где её не было бы и с правом.
      '/admin/api/sources/1/documents': {
        documents: [
          {
            id: 5,
            sourceId: 1,
            filename: 'prikaz.docx',
            mime: 'text/plain',
            byteSize: 40960,
            uploadedBy: 'редактор',
          },
        ],
      },
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onBack={() => {}} />)
    // Строка документа показана — значит скрыта именно кнопка, а не раздел.
    expect(await screen.findByText('prikaz.docx')).toBeTruthy()
    expect(screen.queryByText('Разобрать моделью')).toBeNull()
    expect(screen.getByText(/право запускать\s+генерацию/)).toBeTruthy()
  })

  it('человеку без права приёмки не показывает действие и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    render(<SourceScreen me={ЧИТАТЕЛЬ} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText(/Принятые пункты/)).toBeTruthy())
    expect(screen.queryByText('Принять разбор')).toBeNull()
    expect(screen.getByText(/кому выдано право принимать/)).toBeTruthy()
  })

  it('черновик говорит, что для приложения его не существует', async () => {
    // Состояния источника не было вовсе: ставилось умолчание «черновик», а
    // менять его было нечем — и справочник в приложении оставался пуст у
    // всех и всегда.
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
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
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText('Снять с раздачи')).toBeTruthy())
  })

  it('раздел помечен, а запись нет', async () => {
    // По разделу не спрашивают, и составитель, не видя этого, ищет
    // пропавшие задачи в генерации, а не в разборе. У записи род —
    // умолчание, и метка у каждой строки была бы шумом.
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await screen.findByText('Порядок')
    expect(screen.getAllByText('раздел')).toHaveLength(1)
  })

  it('человеку без права приёмки состояние менять нечем', async () => {
    // Решить, что источник теперь учит врача, — то же решение, что принять
    // разбор, и право у них одно.
    render(<SourceScreen me={ЧИТАТЕЛЬ} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Состояние' })).toBeTruthy())
    expect(screen.queryByText('Объявить действующим')).toBeNull()
  })
})

describe('пределы и отбор', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('говорит числом, что показаны не все единицы', async () => {
    // Отбор, показавший пятьсот единиц из четырнадцати тысяч, и отбор,
    // показавший все пятьсот, какие есть, выглядели одинаково — а
    // решения по ним принимаются разные: по первому составитель
    // заказывает генерацию, думая, что видит весь класс.
    serve({
      '/admin/api/sources/1/units': { units: UNITS, limit: 500, more: true },
      '/admin/api/sources/1/jobs': { jobs: [] },
      '/admin/api/cases': { cases: [] },
      '/admin/api/sources/1/documents': { documents: [] },
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    const note = await screen.findByText(/Показаны первые 500/)
    expect(note.textContent).toMatch(/сузьте срез/i)
  })

  it('о полном списке не говорит, что он обрезан', async () => {
    // Иначе «показаны не все» стояло бы над всяким списком, и читать его
    // перестали бы вместе с тем случаем, ради которого оно написано.
    serve({
      '/admin/api/sources/1/units': { units: UNITS, limit: 500, more: false },
      '/admin/api/sources/1/jobs': { jobs: [] },
      '/admin/api/cases': { cases: [] },
      '/admin/api/sources/1/documents': { documents: [] },
      '/admin/api/sources/1': SOURCE,
    })
    render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
    await waitFor(() => expect(screen.getByText('3.2')).toBeTruthy())
    expect(screen.queryByText(/Показаны первые/)).toBeNull()
  })

  it('набранный отбор не шлёт запрос на каждую клавишу', async () => {
    // Отбор висел прямо на onChange: «F31.2» — двадцать обращений за
    // секунду, и все, кроме последнего, заказаны за то, чего составитель
    // уже не ищет.
    vi.useFakeTimers()
    try {
      serve({
        '/admin/api/sources/1/units': { units: UNITS, more: false },
        '/admin/api/sources/1/jobs': { jobs: [] },
        '/admin/api/cases': { cases: [] },
        '/admin/api/sources/1/documents': { documents: [] },
        '/admin/api/sources/1': SOURCE,
      })
      render(<SourceScreen me={РЕДАКТОР} id={1} onTitle={() => {}} onBack={() => {}} />)
      // Через act: перечитывание заводится эффектом React, и без него
      // эффект не сольётся, а проверка покажет «запрос не ушёл» там, где
      // он ушёл бы у человека.
      await act(() => vi.advanceTimersByTimeAsync(400))

      const fetched = () =>
        (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.filter(
          (call) => String(call[0]).includes('/units'),
        ).length
      const before = fetched()

      const field = screen.getByPlaceholderText(/например/)
      for (const value of ['F', 'F3', 'F31', 'F31.', 'F31.2']) {
        fireEvent.change(field, { target: { value } })
        await act(() => vi.advanceTimersByTimeAsync(50))
      }
      expect(fetched()).toBe(before)

      // А остановившись — уходит, и ровно один раз.
      await act(() => vi.advanceTimersByTimeAsync(400))
      expect(fetched()).toBe(before + 1)
    } finally {
      vi.useRealTimers()
    }
  })
})
