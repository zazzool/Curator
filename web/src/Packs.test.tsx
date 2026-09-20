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
    { slug: 'cardio', title: 'Кардиология', status: 'published', cases: 2, version: 3 },
    { slug: 'nevro', title: 'Неврология', status: 'draft', cases: 0, version: 0 },
  ],
}

const КАРТОЧКА = {
  slug: 'cardio',
  title: 'Кардиология',
  summaryMd: 'Сорок задач',
  status: 'published',
  version: 3,
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
