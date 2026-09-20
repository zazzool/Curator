import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Cases } from './Cases'
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
      segments: [
        { text: 'Заявление подано в понедельник.', statements: ['абз. 1'] },
        { text: 'Заявитель ждёт ответа.' },
      ],
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
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      const key = Object.keys(answers).find((k) => path.startsWith(k))
      return Promise.resolve(new Response(JSON.stringify(key ? answers[key] : {}), { status: 200 }))
    }),
  )
}

const СОСТАВИТЕЛЬ: Me = {
  login: 'составитель',
  displayName: 'Составитель',
  permissions: ['case:read', 'case:write'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['case:read'] }

describe('задачи источника', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('показывает состояние словами составителя', async () => {
    serve({ '/admin/api/cases': { cases: [задача({ statusWord: 'раздаётся', status: 'published' })] } })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)
    await waitFor(() => expect(screen.getByText('раздаётся')).toBeTruthy())
  })

  it('человеку без права правки не показывает выпуск и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    serve({ '/admin/api/cases': { cases: [задача()] } })
    render(<Cases me={ЧИТАТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Срок рассмотрения')).toBeTruthy())
    expect(screen.queryByText('Раздавать')).toBeNull()
    expect(screen.getByText(/кому выдано право править/)).toBeTruthy()
  })

  it('показывает все замечания публикации разом, а не первое', async () => {
    // Составитель правит задачу в один заход, и отказ, называющий одну
    // беду из четырёх, заставляет его ходить по кругу ровно четыре раза.
    vi.stubGlobal(
      'fetch',
      vi.fn((_path: string, init?: RequestInit) => {
        if ((init?.method ?? 'GET') === 'POST') {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                error: 'Задачу пока нельзя раздавать — вот что мешает',
                faults: [
                  { where: 'разбор', what: 'пустой' },
                  { where: 'ответ', what: 'не совпадает ни с одним вариантом' },
                ],
              }),
              { status: 400 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify({ cases: [задача()] }), { status: 200 }),
        )
      }),
    )
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Раздавать')).toBeTruthy())
    fireEvent.click(screen.getByText('Раздавать'))

    await waitFor(() => expect(screen.getByText(/пустой/)).toBeTruthy())
    expect(screen.getByText(/не совпадает ни с одним вариантом/)).toBeTruthy()
  })

  it('на снятии говорит, что задача осталась в базе', async () => {
    // Попытки по ней уже записаны, и исчезнувшая задача испортила бы
    // отчёты задним числом. Составитель должен это знать до нажатия — и
    // после него.
    serve({
      '/admin/api/cases/': задача({ status: 'archived', statusWord: 'снята с раздачи' }),
      '/admin/api/cases': { cases: [задача({ status: 'published', statusWord: 'раздаётся' })] },
    })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Снять с раздачи')).toBeTruthy())
    fireEvent.click(screen.getByText('Снять с раздачи'))

    await waitFor(() => expect(screen.getByText(/не удалена/)).toBeTruthy())
  })

  it('снятие с раздачи спрашивают, и отказ его останавливает', async () => {
    // Снятая задача пропадает у врачей из ленты и из повторения сразу, а
    // кнопка выглядела ровно как «Открыть». Отказ обязан останавливать
    // действие ДО сервера: остановка после отказа — это уже не вопрос.
    const запросы: string[] = []
    vi.stubGlobal('confirm', vi.fn(() => false))
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        запросы.push(path)
        return Promise.resolve(
          new Response(
            JSON.stringify({ cases: [задача({ status: 'published', statusWord: 'раздаётся' })] }),
            { status: 200 },
          ),
        )
      }),
    )
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Снять с раздачи')).toBeTruthy())
    fireEvent.click(screen.getByText('Снять с раздачи'))

    expect(window.confirm).toHaveBeenCalled()
    await new Promise((done) => setTimeout(done, 0))
    expect(запросы.some((one) => one.includes('/withdraw'))).toBe(false)
  })

  it('разметку показывает прямо в условии, а не сноской под ним', async () => {
    serve({
      '/admin/api/cases/': задача(),
      '/admin/api/cases': { cases: [задача()] },
    })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    fireEvent.click(screen.getByText('Открыть'))

    const marked = await screen.findByText(/абз\. 1/)
    expect(marked.closest('p')?.textContent).toMatch(/Заявление подано в понедельник/)
    expect(screen.getByText(/верный ответ/)).toBeTruthy()
  })

  it('задача правится и сохраняется со своей редакцией', async () => {
    // До этой правки право «править задачи» обещало словами то, чего
    // студия не умела вовсе: написанное моделью можно было только
    // прочитать.
    const посланное: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          посланное.push(JSON.parse(String(init.body)))
          return Promise.resolve(
            new Response(JSON.stringify(задача({ revision: 2 })), { status: 200 }),
          )
        }
        if (path.startsWith('/admin/api/cases/')) {
          return Promise.resolve(new Response(JSON.stringify(задача()), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify({ cases: [задача()] }), { status: 200 }),
        )
      }),
    )
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    fireEvent.click(screen.getByText('Открыть'))
    fireEvent.click(await screen.findByText('Править'))

    const поле = await screen.findByDisplayValue('Заявление подано в понедельник.')
    fireEvent.change(поле, { target: { value: 'Заявление подано во вторник.' } })
    fireEvent.click(screen.getByText('Сохранить задачу'))

    await waitFor(() => expect(посланное.length).toBe(1))
    const ушло = посланное[0] as { revision: number; body: { segments: { text: string }[] } }
    // Редакция уезжает на сервер: сверяет её он, а не студия — двое,
    // открывшие задачу разом, иначе затрут друг друга молча.
    expect(ушло.revision).toBe(1)
    expect(ушло.body.segments[0]!.text).toBe('Заявление подано во вторник.')
    // Разбиение на фрагменты цело: на нём держится разметка.
    expect(ушло.body.segments).toHaveLength(2)
  })

  it('раздаваемая задача не правится, и сказано почему', async () => {
    // Правка раздаваемой молча меняет то, что уже видят на устройствах, и
    // расходится с попытками, записанными по прежнему тексту.
    const живая = задача({ status: 'published', statusWord: 'раздаётся' })
    serve({
      '/admin/api/cases/': живая,
      '/admin/api/cases': { cases: [живая] },
    })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    fireEvent.click(screen.getByText('Открыть'))
    expect(await screen.findByText(/Снимите её с раздачи/)).toBeTruthy()
    expect(screen.queryByText('Править')).toBeNull()
  })

  it('недописанная моделью задача открывается, а не роняет экран', async () => {
    // Поля тела объявлены обязательными, но пишет их модель, и
    // недописанное она отдаёт молча. Перебор отсутствующего бросает во
    // время отрисовки, а отказ отрисовки снимает дерево целиком.
    const кривая = задача({
      body: { title: 'Без вариантов' } as unknown as Case['body'],
    })
    serve({
      '/admin/api/cases/': кривая,
      '/admin/api/cases': { cases: [кривая] },
    })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)

    await waitFor(() => expect(screen.getByText('Открыть')).toBeTruthy())
    fireEvent.click(screen.getByText('Открыть'))

    // Сверяется именно карточка, а не строка списка: на сломанном коде
    // отрисовка бросает, React снимает дерево, и название задачи
    // всё равно находится — в списке, который успел отрисоваться до
    // отказа. Проверка, смотрящая на название, прошла бы на сломанном.
    // «Закрыть» и «редакция» рисует только карточка.
    expect(await screen.findByText('Закрыть')).toBeTruthy()
    expect(screen.getByText(/редакция 1/)).toBeTruthy()
  })

  it('пустой срез объясняет, что делать, а не сообщает «ничего нет»', async () => {
    serve({ '/admin/api/cases': { cases: [] } })
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)
    await waitFor(() => expect(screen.getByText(/Закажите первую/)).toBeTruthy())
  })

  it('раздел, не прочитавший своё, не роняет остальной экран', async () => {
    serve({})
    render(<Cases me={СОСТАВИТЕЛЬ} source={SOURCE} path="" />)
    await waitFor(() => expect(screen.getByText('Задачи')).toBeTruthy())
  })
})
