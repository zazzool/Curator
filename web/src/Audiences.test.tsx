import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Audiences } from './Audiences'
import { правилоСловами, условиеСловами } from './traits'
import type { Me } from './api'

const ОПЕРАТОР: Me = {
  login: 'operator',
  displayName: 'Оператор',
  permissions: ['clients'],
}
const ЧИТАТЕЛЬ: Me = { login: 'reader', displayName: 'Читатель', permissions: [] }

const ГРУППЫ = {
  audiences: [
    {
      slug: 'kafedra',
      title: 'Кафедра терапии',
      note: 'Ординаторы',
      rule: [],
      members: 12,
      broken: '',
    },
    {
      slug: 'ushedshie',
      title: 'Ушедшие плательщики',
      note: '',
      rule: [{ trait: 'ever-paid' }, { trait: 'subscribed', not: true }],
      members: 0,
      broken: '',
    },
    {
      slug: 'slomannaya',
      title: 'Сломанная',
      note: '',
      rule: [],
      members: 0,
      broken: 'признака "vip" не бывает',
    },
  ],
  traits: [],
  windows: [],
}

function serve(answers: [string, unknown][]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      const found = answers.find(([key]) => path.startsWith(key))
      return Promise.resolve(new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }))
    }),
  )
}

describe('группы врачей', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve([
      ['/admin/api/audiences/kafedra/members', { members: [] }],
      ['/admin/api/audiences/kafedra', ГРУППЫ.audiences[0]],
      ['/admin/api/audiences', ГРУППЫ],
    ])
  })

  it('пустое правило названо пустым, а не «все»', async () => {
    // Прочитанное как «все», пустое правило однажды раздаст набор всем
    // подряд, и заметят это по непришедшим деньгам.
    render(<Audiences me={ОПЕРАТОР} />)
    // Групп с пустым правилом в списке две, и обе обязаны говорить об
    // этом одинаково: findAllByText, а не findByText.
    expect((await screen.findAllByText(/только названные поимённо/)).length).toBe(2)
  })

  it('правило читается словами, а не кодами признаков', async () => {
    // Составитель решает, кому раздавать задачи, и решает по этой
    // строке. «ever-paid и not subscribed» он не прочтёт.
    render(<Audiences me={ОПЕРАТОР} />)
    expect(await screen.findByText('когда-либо платил и подписки нет')).toBeTruthy()
  })

  it('сломанное правило видно в списке, а не только в карточке', async () => {
    // Сломанная группа ничего не открывает и ничего не скрывает, и
    // узнать об этом надо раньше, чем врач спросит, куда делся набор.
    render(<Audiences me={ОПЕРАТОР} />)
    expect(await screen.findByText('правило не разобрано')).toBeTruthy()
  })

  it('пустой раздел объясняет, чем раздаются наборы, пока групп нет', async () => {
    serve([['/admin/api/audiences', { audiences: [], traits: [], windows: [] }]])
    render(<Audiences me={ОПЕРАТОР} />)
    expect(await screen.findByText(/раздаются одной только\s+линейкой/)).toBeTruthy()
  })

  it('без права клиентов правку не предлагают', async () => {
    // Раздел виден всем — закрывает его право на сервере, — но кнопка,
    // которая всё равно ответит отказом, не рисуется: нажатая, она
    // выглядит как поломка студии.
    render(<Audiences me={ЧИТАТЕЛЬ} />)
    await screen.findByText('Кафедра терапии')
    expect(screen.queryByText('Завести группу')).toBeNull()
  })

  it('размер считается по нажатию, а не при открытии карточки', async () => {
    // Счёт проходит по всем учётным записям, а у группы с поведенческим
    // признаком — ещё и по всем попыткам. Считай его открытие карточки,
    // раздел дорожал бы ровно от того, что врачей становится больше.
    serve([
      ['/admin/api/audiences/kafedra/size', { size: 12 }],
      ['/admin/api/audiences/kafedra/members', { members: [] }],
      ['/admin/api/audiences/kafedra', ГРУППЫ.audiences[0]],
      ['/admin/api/audiences', ГРУППЫ],
    ])
    render(<Audiences me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Кафедра терапии'))
    await screen.findByText('Названные поимённо')
    expect(screen.queryByText(/Сейчас попадает/)).toBeNull()

    fireEvent.click(screen.getByText('Посчитать, сколько попадает'))
    expect(await screen.findByText(/Сейчас попадает 12 врачей/)).toBeTruthy()
  })

  it('номер врача не число — отказ до обращения к серверу', async () => {
    // Иначе сервер ответит «врача с номером 0 нет», и оператор пойдёт
    // искать врача вместо того, чтобы посмотреть, что он набрал.
    const fetched = vi.fn((path: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(
            path.startsWith('/admin/api/audiences/kafedra/members')
              ? { members: [] }
              : path.startsWith('/admin/api/audiences/kafedra')
                ? ГРУППЫ.audiences[0]
                : ГРУППЫ,
          ),
          { status: 200 },
        ),
      ),
    )
    vi.stubGlobal('fetch', fetched)

    render(<Audiences me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Кафедра терапии'))
    await screen.findByText('Названные поимённо')
    const before = fetched.mock.calls.length

    fireEvent.change(screen.getByPlaceholderText('№ врача'), { target: { value: 'иванов' } })
    fireEvent.click(screen.getByText('Добавить врача'))

    await waitFor(() => expect(screen.getByText(/Номер врача — это целое число/)).toBeTruthy())
    expect(fetched.mock.calls.length).toBe(before)
  })
})

describe('признаки словами', () => {
  it('отрицание пишется своими словами, а не приставкой «не»', () => {
    // «не почта привязана» — это не по-русски, и читать такое правило
    // составитель не станет.
    expect(условиеСловами({ trait: 'email-bound', not: true })).toBe('почта не привязана')
    expect(условиеСловами({ trait: 'ever-paid', not: true })).toBe('не платил ни разу')
  })

  it('порог и окно приезжают в строке, а не теряются', () => {
    // Правило «решил не меньше задач» без числа и срока не значит
    // ничего, а выглядит осмысленным.
    expect(условиеСловами({ trait: 'solved-min', n: 100, over: '30d' })).toBe(
      'решил не меньше задач 100 задач за 30 суток',
    )
  })

  it('непонятный признак показывается как есть', () => {
    // Записан он в базе, и скрыть его от составителя значит оставить
    // его чинить правило вслепую.
    expect(условиеСловами({ trait: 'vip' })).toBe('непонятный признак «vip»')
  })

  it('правило из нескольких признаков соединяется «и»', () => {
    // «Или» в правиле нет: оно делается тем, что набор открыт сразу
    // нескольким группам, и тогда видно его на карточке набора.
    expect(
      правилоСловами([{ trait: 'ever-paid' }, { trait: 'solved-min', n: 50 }]),
    ).toBe('когда-либо платил и решил не меньше задач 50 задач за всё время')
  })
})

describe('правка правила', () => {
  it('смена признака уносит порог, которого новый признак не берёт', async () => {
    // Оставь порог — и правило уехало бы на сервер с числом, которое
    // сервер отвергает, а составитель видел бы признак без поля и не
    // понимал, о каком пороге речь.
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ audiences: [], traits: [], windows: [] }), {
            status: 200,
          }),
        ),
      ),
    )
    render(<Audiences me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Завести группу'))
    fireEvent.click(screen.getByText('Добавить признак'))

    // Единица меры стоит в той же подписи, и читающий с экрана слышит
    // «Порог задач» — то есть чего именно порог.
    fireEvent.change(screen.getByLabelText('Признак'), { target: { value: 'solved-min' } })
    fireEvent.change(screen.getByLabelText(/Порог/), { target: { value: '50' } })
    expect(screen.getByText(/решил не меньше задач 50 задач/)).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Признак'), { target: { value: 'email-bound' } })
    expect(screen.queryByLabelText(/Порог/)).toBeNull()
    expect(screen.getByText(/Читается так: почта привязана$/)).toBeTruthy()
  })
})
