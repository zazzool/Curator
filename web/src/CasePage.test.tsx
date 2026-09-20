import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CasePage } from './CasePage'
import type { Case, Me } from './api'

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

function serve(one: Case) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(JSON.stringify(one), { status: 200 }))),
  )
}

const СОСТАВИТЕЛЬ: Me = {
  login: 'составитель',
  displayName: 'Составитель',
  permissions: ['case:read', 'case:write'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['case:read'] }

describe('страница задачи', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/cases/c-abcdefgh23456789')
  })

  it('показывает паспорт с опознавателем и единицей источника', async () => {
    // Открывший задачу по ссылке из чужого письма первым делом
    // спрашивает «какая это и в каком она состоянии», а уже потом читает
    // текст. Опознаватель показывается целиком: его переписывают обратно
    // в письма.
    serve(задача())
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    await waitFor(() => expect(screen.getByText('c-abcdefgh23456789')).toBeTruthy())
    expect(screen.getByText('3/3.1')).toBeTruthy()
    expect(screen.getByText('черновик')).toBeTruthy()
  })

  it('разметку показывает прямо в условии, а не сноской под ним', async () => {
    // Составитель проверяет именно её — какой фрагмент какое положение
    // подтверждает, — и сноска заставила бы считать фрагменты глазами.
    serve(задача())
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    const marked = await screen.findByText(/абз\. 1/)
    expect(marked.closest('p')?.textContent).toMatch(/Заявление подано в понедельник/)
    expect(screen.getByText(/верный ответ/)).toBeTruthy()
  })

  it('задача правится и сохраняется со своей редакцией', async () => {
    const посланное: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((_path: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          посланное.push(JSON.parse(String(init.body)))
          return Promise.resolve(
            new Response(JSON.stringify(задача({ revision: 2 })), { status: 200 }),
          )
        }
        return Promise.resolve(new Response(JSON.stringify(задача()), { status: 200 }))
      }),
    )
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

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
    serve(задача({ status: 'published', statusWord: 'раздаётся' }))
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    expect(await screen.findByText(/Снимите её с раздачи/)).toBeTruthy()
    expect(screen.queryByText('Править')).toBeNull()
  })

  it('показывает все замечания выпуска разом, а не первое', async () => {
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
        return Promise.resolve(new Response(JSON.stringify(задача()), { status: 200 }))
      }),
    )
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    fireEvent.click(await screen.findByText('Раздавать'))
    await waitFor(() => expect(screen.getByText(/пустой/)).toBeTruthy())
    expect(screen.getByText(/не совпадает ни с одним вариантом/)).toBeTruthy()
  })

  it('снятие с раздачи спрашивают, и отказ его останавливает', async () => {
    // Снятая задача пропадает у врачей из ленты и из повторения сразу.
    // Отказ обязан останавливать действие ДО сервера: остановка после
    // отказа — это уже не вопрос.
    const запросы: string[] = []
    vi.stubGlobal('confirm', vi.fn(() => false))
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        запросы.push(path)
        return Promise.resolve(
          new Response(
            JSON.stringify(задача({ status: 'published', statusWord: 'раздаётся' })),
            { status: 200 },
          ),
        )
      }),
    )
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    fireEvent.click(await screen.findByText('Снять с раздачи'))
    expect(window.confirm).toHaveBeenCalled()
    await new Promise((done) => setTimeout(done, 0))
    expect(запросы.some((one) => one.includes('/withdraw'))).toBe(false)
  })

  it('на снятии говорит, что задача осталась в базе', async () => {
    // Попытки по ней уже записаны, и исчезнувшая задача испортила бы
    // отчёты задним числом.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              path.includes('/withdraw')
                ? задача({ status: 'archived', statusWord: 'снята с раздачи' })
                : задача({ status: 'published', statusWord: 'раздаётся' }),
            ),
            { status: 200 },
          ),
        ),
      ),
    )
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    fireEvent.click(await screen.findByText('Снять с раздачи'))
    await waitFor(() => expect(screen.getByText(/не удалена/)).toBeTruthy())
  })

  it('человеку без права правки не показывает выпуск и говорит, почему', async () => {
    // Страница при этом открыта: право проверяет сервер, а спрятанная
    // страница — подсказка, где искать, а не запрет.
    serve(задача())
    render(<CasePage me={ЧИТАТЕЛЬ} id="c-abcdefgh23456789" />)

    await waitFor(() => expect(screen.getByText('Срок рассмотрения')).toBeTruthy())
    expect(screen.queryByText('Раздавать')).toBeNull()
    expect(screen.getByText(/кому выдано право править/)).toBeTruthy()
  })

  it('недописанная моделью задача открывается, а не роняет страницу', async () => {
    // Поля тела объявлены обязательными, но пишет их модель, и
    // недописанное она отдаёт молча. Перебор отсутствующего бросает во
    // время отрисовки, а отказ отрисовки снимает дерево целиком.
    //
    // Сверяется то, что рисует именно карточка задачи: заголовок «Задача»
    // и кнопка «К задачам» стоят снаружи и отрисовались бы и на
    // сломанном.
    serve(задача({ body: { title: 'Без вариантов' } as unknown as Case['body'] }))
    render(<CasePage me={СОСТАВИТЕЛЬ} id="c-abcdefgh23456789" />)

    expect(await screen.findByText('c-abcdefgh23456789')).toBeTruthy()
    expect(screen.getByText('Без вариантов')).toBeTruthy()
  })
})
