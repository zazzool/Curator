import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Packs } from './Packs'
import type { Me } from './api'

const СОСТАВИТЕЛЬ: Me = {
  login: 'составитель',
  displayName: 'Составитель',
  permissions: ['packs', 'case:read'],
}
const ЧИТАТЕЛЬ: Me = { login: 'читатель', displayName: 'Читатель', permissions: ['case:read'] }

const НАБОРЫ = {
  packs: [
    { slug: 'cardio', title: 'Кардиология', status: 'published', line: 'paid', cases: 2, version: 3 },
    { slug: 'nevro', title: 'Неврология', status: 'draft', line: 'guest', cases: 0, version: 0 },
  ],
}

const КАРТОЧКА = {
  slug: 'cardio',
  title: 'Кардиология',
  summaryMd: 'Сорок задач',
  status: 'published',
  line: 'paid',
  version: 3,
  revision: 7,
  cases: [
    { id: 'c-1', ord: 0, title: 'Боль за грудиной', unitLabel: 'I21', status: 'published' },
    { id: 'c-2', ord: 1, title: 'Одышка', unitLabel: 'I50', status: 'published' },
  ],
}

// Ответы сервера подставляются по началу адреса, и порядок ключей значим:
// «/admin/api/packs» начинает собой и «/admin/api/packs/cardio». Точный
// адрес стоит первым, иначе карточка получила бы список.
function serve(answers: [string, unknown][]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      const found = answers.find(([key]) => path.startsWith(key))
      return Promise.resolve(
        new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
      )
    }),
  )
}

describe('наборы', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve([
      ['/admin/api/packs/cardio', КАРТОЧКА],
      ['/admin/api/packs', НАБОРЫ],
      ['/admin/api/cases', { cases: [] }],
    ])
  })

  it('невыпущенный набор назван невыпущенным, а не набором с выпуском 0', async () => {
    // «выпуск 0» составитель прочтёт как номер выпуска, а не как его
    // отсутствие, — и будет ждать на устройствах то, чего там нет.
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    expect(await screen.findByText(/не выпускался/)).toBeTruthy()
    expect(screen.getByText(/выпуск 3/)).toBeTruthy()
  })

  it('пустая витрина объясняет, что вкладка наборов в приложении пуста', async () => {
    serve([
      ['/admin/api/packs', { packs: [] }],
      ['/admin/api/cases', { cases: [] }],
    ])
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    expect(await screen.findByText(/вкладка «Наборы» в приложении пуста/)).toBeTruthy()
  })

  it('человеку без права наборов не показывает действие и говорит, почему', async () => {
    // Раздел при этом открыт: право проверяет сервер, а спрятанный раздел —
    // подсказка, где искать, а не запрет.
    render(<Packs me={ЧИТАТЕЛЬ} />)
    await screen.findByText('Кардиология')
    expect(screen.queryByText('Завести набор')).toBeNull()
    expect(screen.getByText(/кому выдано право «наборы»/)).toBeTruthy()
  })

  it('состав читается названиями задач, а не одними номерами', async () => {
    // Состав можно было только переписать, но не прочесть: сервер отдавал
    // лишь число. Собирать его вслепую значит затирать чужую работу.
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    expect(await screen.findByText('Боль за грудиной')).toBeTruthy()
    expect(screen.getByText('Одышка')).toBeTruthy()
  })

  it('переставленный состав объявлен несохранённым', async () => {
    // Ушедший с несохранённым составом теряет свою работу и узнаёт об этом,
    // только вернувшись. Сказать надо в тот же миг.
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    await screen.findByText('Одышка')
    expect(screen.queryByText(/пока не сохранён/)).toBeNull()

    fireEvent.click(screen.getAllByText('↓')[0]!)
    expect(screen.getByText(/пока не сохранён/)).toBeTruthy()
  })

  it('сохранение состава шлёт новый порядок, а не только гасит подсказку', async () => {
    // Прежняя проверка сверяла только появление подсказки: сделай
    // saveItems пустым — и она прошла бы. Самая разрушительная запись в
    // студии не проверялась ничем, а переставленный вручную состав
    // сорока задач теряется целиком и молча.
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    await screen.findByText('Одышка')

    fireEvent.click(screen.getAllByText('↓')[0]!)
    fireEvent.click(screen.getByText('Сохранить состав'))

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) =>
          String(call[0]).includes('/admin/api/packs/cardio/items') &&
          (call[1] as { method?: string })?.method === 'PUT',
      )
      expect(sent).toBeTruthy()
      const body = JSON.parse(String((sent![1] as { body?: string }).body))
      // Именно переставленный порядок, а не тот, что пришёл с сервера.
      expect(body.cases).toEqual(['c-2', 'c-1'])
      // И та редакция, на которой состав открывали: без неё сервер не
      // отличит правку от затирания чужой.
      expect(body.revision).toBe(7)
    })
  })

  it('линейка набора видна в списке и уходит при сохранении карточки', async () => {
    // Линейка — это граница бесплатного, и решается она в студии. Не
    // покажи список, кому набор открыт, — и платный корпус, розданный
    // гостевой линейкой, не увидит никто: молча раздающий набор выглядит
    // ровно как молча закрытый.
    let ушло: Record<string, unknown> | null = null
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path === '/admin/api/packs/cardio') {
          ушло = JSON.parse(String(init.body))
        }
        const found: [string, unknown][] = [
          ['/admin/api/packs/cardio', КАРТОЧКА],
          ['/admin/api/packs', НАБОРЫ],
          ['/admin/api/cases', { cases: [] }],
        ]
        const one = found.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(one ? one[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    // В списке — словом, а не меткой линейки на латинице.
    expect(await screen.findByText('платный')).toBeTruthy()
    expect(screen.getByText('гостевой')).toBeTruthy()

    fireEvent.click(screen.getByText('Кардиология'))
    await screen.findByText('Одышка')
    const выбор = screen.getByLabelText('Кому открыт') as HTMLSelectElement
    expect(выбор.value).toBe('paid')
    fireEvent.change(выбор, { target: { value: 'sponsored' } })
    fireEvent.click(screen.getByText('Сохранить карточку'))

    await waitFor(() => expect(ушло).not.toBeNull())
    const тело = ушло as unknown as Record<string, unknown>
    expect(тело.line).toBe('sponsored')
    expect(тело.revision).toBe(7)
  })

  it('опоздавшая правка не стирает набранное и предлагает перечитать', async () => {
    // Двое правят один набор. Отказ без выхода оставляет составителя с
    // двадцатью минутами перестановок, которые больше никогда не
    // сохранятся; перечитывание само, без спроса, стёрло бы их тут же.
    let перечитано = 0
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.includes('/items')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                error: 'Набор успели поправить: у вас редакция 7, в наборе уже 8',
              }),
              { status: 409 },
            ),
          )
        }
        if (path.startsWith('/admin/api/packs/cardio')) {
          перечитано++
          return Promise.resolve(new Response(JSON.stringify(КАРТОЧКА), { status: 200 }))
        }
        const found: [string, unknown][] = [
          ['/admin/api/packs', НАБОРЫ],
          ['/admin/api/cases', { cases: [] }],
        ]
        const one = found.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(one ? one[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    await screen.findByText('Одышка')
    const прочитаноДо = перечитано

    fireEvent.click(screen.getAllByText('↓')[0]!)
    fireEvent.click(screen.getByText('Сохранить состав'))

    // Слова сервера показаны как есть, и рядом стоит единственный выход.
    expect(await screen.findByText(/в наборе уже 8/)).toBeTruthy()
    const перечитать = screen.getByRole('button', { name: 'Перечитать набор' })
    // До нажатия набор не перечитан: переставленное на месте.
    expect(перечитано).toBe(прочитаноДо)
    expect(screen.getAllByText(/Одышка|Боль за грудиной/)[0]!.textContent).toContain('Одышка')

    fireEvent.click(перечитать)
    await waitFor(() => expect(перечитано).toBeGreaterThan(прочитаноДо))
  })

  it('пустой набор выпустить нечем, и кнопка об этом говорит', async () => {
    // Выпуск пустого набора собрался бы и уехал на устройства пустым: врач
    // скачал бы набор и не нашёл в нём ни одной задачи.
    serve([
      ['/admin/api/packs/nevro', { ...КАРТОЧКА, slug: 'nevro', version: 0, cases: [] }],
      ['/admin/api/packs', НАБОРЫ],
      ['/admin/api/cases', { cases: [] }],
    ])
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Неврология'))
    const выпустить = await screen.findByText('Выпустить')
    expect((выпустить as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/Состав пуст/)).toBeTruthy()
  })

  it('подбор берёт только раздаваемые задачи', async () => {
    // Набор из черновиков соберётся, а врач получит выпуск, половины
    // которого нет ни в ленте, ни в повторении.
    let asked = ''
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        if (path.startsWith('/admin/api/cases')) asked = path
        const body = path.startsWith('/admin/api/packs/cardio')
          ? КАРТОЧКА
          : path.startsWith('/admin/api/packs')
            ? НАБОРЫ
            : { cases: [] }
        return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
      }),
    )
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    fireEvent.click(await screen.findByText('Добавить задачи'))
    await waitFor(() => expect(asked).toContain('status=published'))
  })
})

describe('подбор задач в набор', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  // Подбор берёт двести задач за раз, выбрасывает уже добавленные и
  // раньше во всех случаях говорил одно: «раздаваемых задач не нашлось».
  // При двухстах задачах, все из которых уже в наборе, это утверждение
  // ложно — их могут быть тысячи, — и составитель считал набор собранным.
  async function открытьПодбор() {
    render(<Packs me={СОСТАВИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Кардиология'))
    fireEvent.click(await screen.findByText('Добавить задачи'))
  }

  it('не говорит «не нашлось», когда показанные просто уже в наборе', async () => {
    serve([
      ['/admin/api/packs/cardio', КАРТОЧКА],
      ['/admin/api/packs', НАБОРЫ],
      [
        '/admin/api/cases',
        {
          // Ровно те, что уже в наборе, и сервер говорит, что есть ещё.
          cases: КАРТОЧКА.cases.map((one) => ({
            id: one.id,
            unitLabel: one.unitLabel,
            status: 'published',
            body: { title: one.title },
          })),
          limit: 200,
          more: true,
        },
      ],
    ])
    await открытьПодбор()
    const said = await screen.findByText(/уже в наборе/)
    expect(said.textContent).not.toMatch(/не нашлось/)
    expect(said.textContent).toMatch(/больше, чем подбор берёт за раз/)
  })

  it('о пустом содержании говорит, что задач нет вовсе', async () => {
    serve([
      ['/admin/api/packs/cardio', { ...КАРТОЧКА, cases: [] }],
      ['/admin/api/packs', НАБОРЫ],
      ['/admin/api/cases', { cases: [], limit: 200, more: false }],
    ])
    await открытьПодбор()
    expect(await screen.findByText(/Раздаваемых задач не нашлось/)).toBeTruthy()
  })
})
