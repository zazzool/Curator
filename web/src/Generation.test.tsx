import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Generation } from './Generation'
import type { Draft, Job, Me, Source, Unit } from './api'

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
          ЧЕРНОВИК,
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

  it('черновик принимается задачей — и второй раз не заводит второй', async () => {
    // Здесь конвейер и обрывался: деньги за обращение к модели платились,
    // черновик показывался, а выхода у него не было. Второе нажатие —
    // при обрыве связи или просто дважды — не должно заводить второй
    // задачи с тем же условием.
    const посланные: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path === '/admin/api/cases') {
          посланные.push(JSON.parse(String(init.body)))
          return Promise.resolve(
            new Response(JSON.stringify({ id: 'c-01', repeated: посланные.length > 1 }), {
              status: посланные.length > 1 ? 200 : 201,
            }),
          )
        }
        if (path.startsWith('/admin/api/jobs/7')) {
          return Promise.resolve(
            new Response(JSON.stringify(job({ status: 'done', drafts: [ЧЕРНОВИК] })), {
              status: 200,
            }),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify({ jobs: [job({ status: 'done' })] }), { status: 200 }),
        )
      }),
    )
    render(<Generation me={РЕДАКТОР} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))

    fireEvent.click(await screen.findByText('Принять черновик'))
    expect(await screen.findByText(/Задача заведена/)).toBeTruthy()
    expect(посланные).toEqual([{ draftId: 11 }])

    // Кнопки больше нет: принятый черновик принимать нечем, и второго
    // нажатия неоткуда взяться.
    expect(screen.queryByText('Принять черновик')).toBeNull()
  })

  it('без права правки задач принять черновик не предлагается', async () => {
    // Раздел закрывает право на сервере; здесь скрывается действие,
    // которое всё равно отказало бы, и рядом сказано, почему его нет.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({ status: 'done', drafts: [ЧЕРНОВИК] }),
    })
    render(<Generation me={ЧИТАТЕЛЬ} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))
    expect(await screen.findByText(/право «править задачи»/)).toBeTruthy()
    expect(screen.queryByText('Принять черновик')).toBeNull()
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

  it('опрос прекращается, когда очередь перестала читаться', async () => {
    // Отказ оставлял очередь как была, а признак «идёт работа» считается
    // по ней: опрос раз в три секунды не прекращался НИКОГДА. На
    // истёкшей сессии это тысячи отказов подряд — ровно то, о чём
    // предупреждает пояснение к опросу.
    vi.useFakeTimers()
    const calls: string[] = []
    let первый = true
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        calls.push(path)
        if (!path.includes('/jobs')) {
          return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }))
        }
        if (первый) {
          первый = false
          return Promise.resolve(
            new Response(JSON.stringify({ jobs: [job({ status: 'running' })] }), { status: 200 }),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'Сессия не найдена' }), { status: 401 }),
        )
      }),
    )
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    // Ждём не обращения, а показанной строки: опрос заводится от
    // прочитанной очереди, и до того, как она доедет до состояния,
    // заводить ему нечего.
    await vi.waitFor(() => expect(screen.getByText(/пишется/)).toBeTruthy())
    const доТика = calls.length
    await vi.advanceTimersByTimeAsync(3500)
    expect(calls.length).toBeGreaterThan(доТика)
    await vi.waitFor(() => expect(screen.getByText(/Сессия не найдена/)).toBeTruthy())

    // Счёт снимается после того, как отказ доехал до состояния: до этого
    // опрос ещё жив по праву, и мерить нечего. Дальше опрашивать нечего —
    // сколько бы времени ни прошло.
    await vi.advanceTimersByTimeAsync(3500)
    const послеОтказа = calls.length
    await vi.advanceTimersByTimeAsync(60000)
    expect(calls.length).toBe(послеОтказа)
    vi.useRealTimers()
  })
})

describe('слепая сверка и повтор', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('несогласие сверки показывается тревогой, а не тонет в подсказке', async () => {
    // Вердикт сверки вычислялся и ПРОПАДАЛ: за сверку платили, а
    // составитель её не видел — задача, с которой сверка не согласилась,
    // выглядела ровно как та, с которой согласилась.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({
        status: 'done',
        drafts: [
          {
            ...ЧЕРНОВИК,
            check: {
              done: true,
              verdict: {
                answer: '3.2',
                why: 'В условии назван письменный отказ.',
                sure: true,
                agrees: false,
              },
            },
          },
        ],
      }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))

    const тревога = await screen.findByText(/Слепая сверка НЕ сошлась/)
    expect(тревога.textContent).toMatch(/3\.2/)
    expect(тревога.textContent).toMatch(/письменный отказ/)
  })

  it('согласие сверки не выглядит тревогой', async () => {
    // Тревога у исправной задачи приучает не верить тревоге, и тогда её
    // перестанут читать там, где она заслужена.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({
        status: 'done',
        drafts: [
          {
            ...ЧЕРНОВИК,
            check: { done: true, verdict: { answer: '3.1', why: '', sure: true, agrees: true } },
          },
        ],
      }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))

    expect(await screen.findByText(/Слепая сверка сошлась/)).toBeTruthy()
    expect(screen.queryByText(/НЕ сошлась/)).toBeNull()
  })

  it('несостоявшаяся сверка названа отдельно от несогласия и с причиной', async () => {
    // «Сверки не было» и «сверка не согласна» требуют разного: первое —
    // прочитать задачу самому, второе — разобрать спор. Слитые в одно, они
    // дают «всё чисто» у сотни непроверенных задач подряд.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({
        status: 'done',
        drafts: [
          { ...ЧЕРНОВИК, check: { done: false, note: 'у поставщика кончились деньги' } },
        ],
      }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))

    const сказано = await screen.findByText(/Слепая сверка не состоялась/)
    expect(сказано.textContent).toMatch(/кончились деньги/)
    expect(screen.queryByText(/НЕ сошлась/)).toBeNull()
  })

  it('черновик без сверки не выдаётся за проверенный', async () => {
    // Молчание здесь читается как «всё хорошо», а значит задача, которую
    // не смотрел никто, уходит к врачу с видом проверенной.
    serve({
      '/admin/api/sources/1/jobs': { jobs: [job({ status: 'done' })] },
      '/admin/api/jobs/7': job({ status: 'done', drafts: [ЧЕРНОВИК] }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)
    fireEvent.click(await screen.findByText('Открыть'))

    expect(await screen.findByText(/задачу не проверял никто/)).toBeTruthy()
  })

  it('отказавшее задание можно повторить, а идущее — нет', async () => {
    // До кнопки повтора выхода не было вовсе: ключ повторности запирал
    // единицу навсегда, и повторный заказ молча возвращал прежнее,
    // закрытое задание.
    const calls = serve({
      '/admin/api/sources/1/jobs': {
        jobs: [job({ id: 7, status: 'failed', error: 'модель вернула не тот JSON' })],
      },
      '/admin/api/jobs/7/retry': job({ id: 8, status: 'queued' }),
    })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    fireEvent.click(await screen.findByText('Повторить'))
    await waitFor(() =>
      expect(calls).toContain('POST /admin/api/jobs/7/retry'),
    )
    // Отменить закрытое нечего: отмена и повтор — про разные состояния.
    expect(screen.queryByText('Отменить')).toBeNull()
  })

  it('у идущего задания повтора нет', async () => {
    // Второе задание по той же единице написало бы вторую задачу, и
    // заплачено было бы за обе.
    serve({ '/admin/api/sources/1/jobs': { jobs: [job({ status: 'running' })] } })
    render(<Generation me={СОСТАВИТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('Отменить')).toBeTruthy())
    expect(screen.queryByText('Повторить')).toBeNull()
  })

  it('человеку без права генерации повторять нечем', async () => {
    serve({ '/admin/api/sources/1/jobs': { jobs: [job({ status: 'failed', error: 'отказ' })] } })
    render(<Generation me={ЧИТАТЕЛЬ} source={SOURCE} units={UNITS} />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    expect(screen.queryByText('Повторить')).toBeNull()
  })
})
