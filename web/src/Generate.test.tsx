import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Generate } from './Generate'
import type { Check, Draft, Job, Me, Source, Unit } from './api'

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
    kind: 'case',
    sourceId: 1,
    unitLabel: '3.1',
    status: 'queued',
    step: 'compose',
    stepWord: 'написание',
    attempts: 0,
    error: '',
    notes: [],
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

// Право «править задачи» — не то же самое, что право генерации: заказать
// написание и принять написанное решают разные люди.
const РЕДАКТОР: Me = {
  login: 'редактор',
  displayName: 'Редактор',
  permissions: ['source:read', 'generate', 'case:write'],
}

const ЧЕРНОВИК: Draft = {
  id: 11,
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
}

const ОБЫЧНО = {
  '/admin/api/sources/1/units': { units: UNITS },
  '/admin/api/sources/1/jobs': { jobs: [] },
  '/admin/api/sources': { sources: [SOURCE] },
}

describe('страница заказа задачи', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/generate')
  })

  it('открывается без источника в адресе и берёт первый', async () => {
    // «Создать задачу» — это строка в колонке разделов, и она обязана
    // вести на работающую страницу, а не на форму, у которой ничего не
    // выбрано и потому ничего не показано.
    serve(ОБЫЧНО)
    render(<Generate me={СОСТАВИТЕЛЬ} />)

    // Первый источник выбран, и форма показывает его единицы: страница,
    // открытая из колонки, работает с первого взгляда.
    expect(await screen.findByDisplayValue('Приказ № 1130н')).toBeTruthy()
    expect(screen.getByText('Заказать задачу')).toBeTruthy()
  })

  it('заказывать предлагает только те единицы, по которым есть что спрашивать', async () => {
    // Группа и единица без положений отказали бы на сервере, и список,
    // показывающий их, отправил бы человека за отказом.
    serve(ОБЫЧНО)
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    await waitFor(() => expect(screen.getByText(/3\.1 — Сроки/)).toBeTruthy())
    expect(screen.queryByText(/^3 — Порядок$/)).toBeNull()
  })

  it('зовёт единицу словом источника', async () => {
    // Интерфейс без словаря источника показал бы «единицу» тому, кто ждёт
    // слова «пункт». Ради этого слово и хранится у источника.
    serve(ОБЫЧНО)
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    await waitFor(() => expect(screen.getByText('Выберите пункт')).toBeTruthy())
  })

  it('выбранная единица уходит в адрес', async () => {
    // «Напиши задачу по 3.1» — это ссылка, которую посылают друг другу.
    serve(ОБЫЧНО)
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    const список = await screen.findByDisplayValue('Выберите пункт')
    fireEvent.change(список, { target: { value: '3.1' } })
    expect(window.location.pathname + window.location.search).toBe('/generate?source=1&unit=3.1')
  })

  it('без выбранной единицы заказ не отправляется', async () => {
    // Заказ без единицы отказал бы на сервере, и объяснять его пришлось
    // бы отказом вместо погашенной кнопки.
    const calls = serve(ОБЫЧНО)
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    const кнопка = await screen.findByText('Заказать задачу')
    fireEvent.click(кнопка)
    await new Promise((done) => setTimeout(done, 0))
    expect(calls.some((one) => one.startsWith('POST'))).toBe(false)
  })

  it('человеку без права на генерацию заказ не предлагается, и сказано почему', async () => {
    serve(ОБЫЧНО)
    render(<Generate me={ЧИТАТЕЛЬ} source={1} />)

    await waitFor(() => expect(screen.getByText(/кому выдано право на генерацию/)).toBeTruthy())
    expect(screen.queryByText('Заказать задачу')).toBeNull()
  })

  it('без источников не показывает пустую форму, а зовёт завести источник', async () => {
    // Пустая форма обещала бы действие, которого нет: задача пишется по
    // положениям источника, и без источника заказывать нечего.
    serve({ '/admin/api/sources': { sources: [] } })
    render(<Generate me={СОСТАВИТЕЛЬ} />)
    await waitFor(() => expect(screen.getByText(/Заведите первый/)).toBeTruthy())
  })

  it('показывает состояние словами и называет причину отказа прямо в строке', async () => {
    // «failed» составителю ничего не говорит, а причина, спрятанная за
    // нажатием, не читается никем.
    serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': {
        jobs: [job({ status: 'failed', error: 'модель не ответила' })],
      },
    })
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    await waitFor(() => expect(screen.getByText(/не вышло: модель не ответила/)).toBeTruthy())
  })

  it('черновик принимается задачей, и к ней есть выход', async () => {
    // Здесь конвейер и обрывался: деньги за обращение к модели платились,
    // черновик показывался, а выхода у него не было.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if ((init?.method ?? 'GET') === 'POST' && path === '/admin/api/cases') {
          return Promise.resolve(
            new Response(JSON.stringify({ id: 'c-новая', repeated: false }), { status: 201 }),
          )
        }
        if (path.startsWith('/admin/api/jobs/')) {
          return Promise.resolve(
            new Response(JSON.stringify(job({ status: 'done', drafts: [ЧЕРНОВИК] })), {
              status: 200,
            }),
          )
        }
        if (path.startsWith('/admin/api/sources/1/jobs')) {
          return Promise.resolve(
            new Response(JSON.stringify({ jobs: [job({ status: 'done' })] }), { status: 200 }),
          )
        }
        if (path.startsWith('/admin/api/sources/1/units')) {
          return Promise.resolve(new Response(JSON.stringify({ units: UNITS }), { status: 200 }))
        }
        return Promise.resolve(new Response(JSON.stringify({ sources: [SOURCE] }), { status: 200 }))
      }),
    )
    render(<Generate me={РЕДАКТОР} source={1} />)

    fireEvent.click(await screen.findByText('Открыть'))
    fireEvent.click(await screen.findByText('Принять черновик'))

    fireEvent.click(await screen.findByText('Открыть её'))
    expect(window.location.pathname).toBe('/cases/c-%D0%BD%D0%BE%D0%B2%D0%B0%D1%8F')
  })

  it('без права правки задач принять черновик не предлагается', async () => {
    serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/': job({ status: 'done', drafts: [ЧЕРНОВИК] }),
    })
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    fireEvent.click(await screen.findByText('Открыть'))
    expect(await screen.findByText(/право «править задачи»/)).toBeTruthy()
    expect(screen.queryByText('Принять черновик')).toBeNull()
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
      render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
      await act(() => vi.advanceTimersByTimeAsync(100))
      const было = calls.filter((one) => one.includes('/jobs')).length

      await act(() => vi.advanceTimersByTimeAsync(10_000))
      expect(calls.filter((one) => one.includes('/jobs')).length).toBe(было)
    } finally {
      vi.useRealTimers()
    }
  })

  it('опрос прекращается, когда очередь перестала читаться', async () => {
    // Признак «идёт работа» считается по прочитанному; оставленный
    // прежний список держал его истинным, и опрос раз в три секунды не
    // прекращался никогда — в том числе на истёкшей сессии, где каждое
    // обращение отвечает отказом.
    vi.useFakeTimers()
    try {
      let живо = true
      const calls: string[] = []
      vi.stubGlobal(
        'fetch',
        vi.fn((path: string) => {
          calls.push(path)
          if (path.startsWith('/admin/api/sources/1/jobs')) {
            if (живо) {
              живо = false
              return Promise.resolve(
                new Response(JSON.stringify({ jobs: [job({ status: 'running' })] }), {
                  status: 200,
                }),
              )
            }
            return Promise.resolve(
              new Response(JSON.stringify({ error: 'Сессия кончилась' }), { status: 400 }),
            )
          }
          if (path.startsWith('/admin/api/sources/1/units')) {
            return Promise.resolve(new Response(JSON.stringify({ units: UNITS }), { status: 200 }))
          }
          return Promise.resolve(
            new Response(JSON.stringify({ sources: [SOURCE] }), { status: 200 }),
          )
        }),
      )
      render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
      // Шагами, а не одним прыжком: React сливает состояние на границе
      // act, и десять секунд разом дали бы три тика ДО того, как
      // погасший признак «идёт работа» доедет до опроса. Проверка
      // прошла бы на сломанном.
      await act(() => vi.advanceTimersByTimeAsync(0))
      await act(() => vi.advanceTimersByTimeAsync(3_100))
      const было = calls.filter((one) => one.includes('/jobs')).length

      await act(() => vi.advanceTimersByTimeAsync(10_000))
      expect(calls.filter((one) => one.includes('/jobs')).length).toBe(было)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('слепая сверка и повтор', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/generate')
  })

  /** Задание с одним черновиком, у которого сверка такая, какую попросили. */
  function сЧерновиком(check?: Check) {
    const draft = check === undefined ? ЧЕРНОВИК : { ...ЧЕРНОВИК, check }
    return {
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({ status: 'done', drafts: [draft] }),
    }
  }

  it('несогласие сверки показывается тревогой, а не тонет в подсказке', async () => {
    // Вердикт сверки вычислялся и ПРОПАДАЛ: за сверку платили, а
    // составитель её не видел — задача, с которой сверка не согласилась,
    // выглядела ровно как та, с которой согласилась.
    serve(
      сЧерновиком({
        done: true,
        verdict: {
          answer: '3.2',
          why: 'В условии назван письменный отказ.',
          sure: true,
          agrees: false,
        },
      }),
    )
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    fireEvent.click(await screen.findByText('Открыть'))

    const тревога = await screen.findByText(/Слепая сверка НЕ сошлась/)
    expect(тревога.textContent).toMatch(/3\.2/)
    expect(тревога.textContent).toMatch(/письменный отказ/)
  })

  it('согласие сверки не выглядит тревогой', async () => {
    // Тревога у исправной задачи приучает не верить тревоге, и тогда её
    // перестанут читать там, где она заслужена.
    serve(
      сЧерновиком({
        done: true,
        verdict: { answer: '3.1', why: '', sure: true, agrees: true },
      }),
    )
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    fireEvent.click(await screen.findByText('Открыть'))

    expect(await screen.findByText(/Слепая сверка сошлась/)).toBeTruthy()
    expect(screen.queryByText(/НЕ сошлась/)).toBeNull()
  })

  it('несостоявшаяся сверка названа отдельно от несогласия и с причиной', async () => {
    // «Сверки не было» и «сверка не согласна» требуют разного: первое —
    // прочитать задачу самому, второе — разобрать спор. Слитые в одно, они
    // дают «всё чисто» у сотни непроверенных задач подряд.
    serve(сЧерновиком({ done: false, note: 'у поставщика кончились деньги' }))
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    fireEvent.click(await screen.findByText('Открыть'))

    const сказано = await screen.findByText(/Слепая сверка не состоялась/)
    expect(сказано.textContent).toMatch(/кончились деньги/)
    expect(screen.queryByText(/НЕ сошлась/)).toBeNull()
  })

  it('черновик без сверки не выдаётся за проверенный', async () => {
    // Молчание здесь читается как «всё хорошо», а значит задача, которую
    // не смотрел никто, уходит к врачу с видом проверенной.
    serve(сЧерновиком())
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)
    fireEvent.click(await screen.findByText('Открыть'))

    expect(await screen.findByText(/задачу не проверял никто/)).toBeTruthy()
  })

  it('отказавшее задание можно повторить', async () => {
    // До кнопки повтора выхода не было вовсе: ключ повторности запирал
    // единицу навсегда, и повторный заказ молча возвращал прежнее,
    // закрытое задание.
    const calls = serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': {
        jobs: [job({ id: 7, status: 'failed', error: 'модель вернула не тот JSON' })],
      },
      '/admin/api/jobs/7/retry': job({ id: 8, status: 'queued' }),
    })
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    fireEvent.click(await screen.findByText('Повторить'))
    await waitFor(() => expect(calls).toContain('POST /admin/api/jobs/7/retry'))
    // Отменить закрытое нечего: отмена и повтор — про разные состояния.
    expect(screen.queryByText('Отменить')).toBeNull()
  })

  it('у идущего задания повтора нет', async () => {
    // Второе задание по той же единице написало бы вторую задачу, и
    // заплачено было бы за обе.
    serve({ ...ОБЫЧНО, '/admin/api/sources/1/jobs': { jobs: [job({ status: 'running' })] } })
    render(<Generate me={СОСТАВИТЕЛЬ} source={1} />)

    await waitFor(() => expect(screen.getByText('Отменить')).toBeTruthy())
    expect(screen.queryByText('Повторить')).toBeNull()
  })

  it('человеку без права генерации повторять нечем', async () => {
    serve({
      ...ОБЫЧНО,
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'failed', error: 'отказ' })] },
    })
    render(<Generate me={ЧИТАТЕЛЬ} source={1} />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    expect(screen.queryByText('Повторить')).toBeNull()
  })
})
