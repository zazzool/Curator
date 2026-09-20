import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Sales } from './Sales'
import type { Me } from './api'

const ОПЕРАТОР: Me = {
  login: 'оператор',
  displayName: 'Оператор',
  permissions: ['sales', 'clients'],
}
const СМОТРИТЕЛЬ: Me = {
  login: 'смотритель',
  displayName: 'Смотритель',
  permissions: ['clients'],
}

const КЛИЕНТЫ = {
  clients: [
    {
      id: 7,
      email: 'ivanov@example.ru',
      displayName: 'Иванов И.И.',
      createdAt: '2026-09-01T10:00:00Z',
      lastSeen: '2026-09-18T20:00:00Z',
      blocked: false,
      devices: 2,
      rights: 1,
    },
  ],
}

const КАРТОЧКА = KLIENT(false)

function KLIENT(blocked: boolean) {
  return { ...КЛИЕНТЫ.clients[0]!, blocked }
}

// Адреса подставляются по началу строки, и порядок ключей значим:
// «/admin/api/clients» начинает собой и «/admin/api/clients/7/payments».
function serve(answers: [string, unknown][]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) => {
      const found = answers.find(([key]) => path.startsWith(key))
      return Promise.resolve(new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }))
    }),
  )
}

const ОБЫЧНО: [string, unknown][] = [
  ['/admin/api/clients/7/payments', { payments: [] }],
  ['/admin/api/clients/7/entitlements', { entitlements: [] }],
  ['/admin/api/clients/7', КАРТОЧКА],
  ['/admin/api/clients', КЛИЕНТЫ],
  ['/admin/api/prices', { prices: [{ purpose: 'subscription:month', kopecks: 199000, enabled: true, revision: 4 }] }],
  ['/admin/api/packs', { packs: [] }],
]

describe('продажи', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve(ОБЫЧНО)
  })

  it('возврат спрашивает причину и уносит именно её', async () => {
    // Единственное поле, которым возврат объясняется в разбирательстве,
    // прежде не спрашивалось вовсе: во все возвраты, какие когда-либо
    // будут, вписывалось одно «возврат оформлен в студии».
    let ушло: unknown = null
    const ПЛАТЁЖ = {
      payments: [
        {
          id: 41,
          purpose: 'subscription:month',
          kopecks: 199000,
          status: 'paid',
          source: 'operator',
          at: '2026-09-10T10:00:00Z',
          by: 'оператор',
        },
      ],
    }
    const ответы: [string, unknown][] = [['/admin/api/clients/7/payments', ПЛАТЁЖ], ...ОБЫЧНО]
    vi.stubGlobal('prompt', vi.fn(() => '  ошиблись назначением, деньги вернули на карту  '))
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/refund')) {
          ушло = JSON.parse(String(init.body))
        }
        const found = ответы.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    fireEvent.click(await screen.findByRole('button', { name: 'Вернуть' }))

    await waitFor(() => expect(ушло).not.toBeNull())
    // Пробелы по краям срезаются: причина «   » — это отсутствие причины.
    expect(ушло).toMatchObject({ note: 'ошиблись назначением, деньги вернули на карту' })
  })

  it('возврат без причины на сервер не уходит', async () => {
    let posted = 0
    const ПЛАТЁЖ = {
      payments: [
        {
          id: 41,
          purpose: 'subscription:month',
          kopecks: 199000,
          status: 'paid',
          source: 'operator',
          at: '2026-09-10T10:00:00Z',
          by: 'оператор',
        },
      ],
    }
    const ответы: [string, unknown][] = [['/admin/api/clients/7/payments', ПЛАТЁЖ], ...ОБЫЧНО]
    // Отказ писать причину и пустая причина — одно и то же: возврат без
    // причины и есть тот возврат, который потом нечем объяснить.
    vi.stubGlobal('prompt', vi.fn(() => '   '))
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/refund')) posted++
        const found = ответы.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    fireEvent.click(await screen.findByRole('button', { name: 'Вернуть' }))

    await new Promise((done) => setTimeout(done, 0))
    expect(posted).toBe(0)
  })

  it('цена показывается рублями, а не копейками', async () => {
    // В базе цена лежит копейками намеренно, а оператор думает рублями.
    // Показанные копейки он прочтёт как цену — и она будет в сто раз не та.
    render(<Sales me={ОПЕРАТОР} />)
    expect(await screen.findByText('1 990,00 ₽')).toBeTruthy()
  })

  it('клиент находится и открывается карточкой', async () => {
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    expect(await screen.findByRole('heading', { name: 'Иванов И.И.' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Оформить приход' })).toBeTruthy()
  })

  it('врач без живых прав назван так прямо, а не пустым списком', async () => {
    // Пустой список оператор прочтёт как «не прочиталось», и пойдёт
    // искать отказ там, где всё в порядке: у врача просто нет платного.
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    expect(await screen.findByText(/платного доступа у этого врача сейчас нет/)).toBeTruthy()
  })

  it('без права продаж приход не предлагается, а карточка открыта', async () => {
    render(<Sales me={СМОТРИТЕЛЬ} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    expect(await screen.findByText('Права')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Оформить приход' })).toBeNull()
  })

  it('негодная сумма прихода не уходит на сервер', async () => {
    // Приход на сотую долю суммы обнаружится не сегодня, и исправлять его
    // придётся возвратом по живым деньгам.
    let posted = 0
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST') posted++
        const found = ОБЫЧНО.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    const поле = await screen.findByPlaceholderText('1990')
    fireEvent.change(поле, { target: { value: 'тысяча' } })
    fireEvent.click(screen.getByRole('button', { name: 'Оформить приход' }))
    // Отказ стоит под полем суммы, а не полосой наверху страницы.
    const отказ = await screen.findByRole('alert')
    expect(отказ.textContent).toMatch(/Рублями и копейками/)
    expect(поле.closest('.form-row')?.contains(отказ)).toBe(true)
    expect(posted).toBe(0)
  })

  it('цена уходит с той редакцией, которую оператор видел на витрине', async () => {
    // Двое открыли витрину: первый ставит 399 рублей, второй сохраняет
    // свою цену следом. Без редакции побеждала последняя запись, и
    // узнавалось это по непришедшим деньгам — когда возвращать поздно.
    let ушло: Record<string, unknown> | null = null
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/prices')) {
          ушло = JSON.parse(String(init.body))
        }
        const found = ОБЫЧНО.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    const поле = await screen.findByPlaceholderText('1990')
    fireEvent.change(поле, { target: { value: '2490' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить цену' }))

    await waitFor(() => expect(ушло).not.toBeNull())
    const тело = ушло as unknown as Record<string, unknown>
    expect(тело.kopecks).toBe(249000)
    // Редакция той строки, которая показана, а не какая-нибудь.
    expect(тело.revision).toBe(4)
  })

  it('цена товара без цены уходит нулевой редакцией', async () => {
    // Нуль означает «цены не было». Пришли с ненулевой — и сервер примет
    // заведение за правку чужой цены, назначенной, пока витрину читали.
    let ушло: Record<string, unknown> | null = null
    const пусто: [string, unknown][] = [
      ['/admin/api/prices', { prices: [] }],
      ...ОБЫЧНО.filter(([key]) => key !== '/admin/api/prices'),
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/prices')) {
          ушло = JSON.parse(String(init.body))
        }
        const found = пусто.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    const поле = await screen.findByPlaceholderText('1990')
    fireEvent.change(поле, { target: { value: '2490' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить цену' }))

    await waitFor(() => expect(ушло).not.toBeNull())
    expect((ушло as unknown as Record<string, unknown>).revision).toBe(0)
  })

  it('приход уносит ровно ту сумму, которую набрал оператор', async () => {
    // Единственная проверка платежа была отрицательной: она смотрела,
    // что негодная сумма НЕ ушла. Подели сумму на сто в `accept` — и
    // всякая продажа занижается стократно, а набор остаётся зелёным.
    // Занижение обнаружится не сегодня, а исправлять его придётся
    // возвратом по живым деньгам.
    let ушло: Record<string, unknown> | null = null
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/payments')) {
          ушло = JSON.parse(String(init.body))
          return Promise.resolve(
            new Response(JSON.stringify({ id: 12, kopecks: 199050 }), { status: 200 }),
          )
        }
        const found = ОБЫЧНО.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    const поле = await screen.findByPlaceholderText('1990')
    fireEvent.change(поле, { target: { value: '1990,50' } })
    fireEvent.change(screen.getByPlaceholderText('перевод от 19.09, чек №…'), {
      target: { value: 'перевод от 20.09' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Оформить приход' }))

    await waitFor(() => expect(ушло).not.toBeNull())
    const тело = ушло as unknown as Record<string, unknown>
    expect(тело.kopecks).toBe(199050)
    expect(тело.accountId).toBe(7)
    expect(тело.purpose).toBe('subscription:month')
    expect(тело.note).toBe('перевод от 20.09')
    // Ключ повторности — обязательство перед оператором, нажавшим второй
    // раз на оборванной связи: без него сервер оформит второй приход, а
    // деньги были одни.
    expect(String(тело.idemKey ?? '')).not.toBe('')
  })

  it('второй приход того же врача идёт своим ключом повторности', async () => {
    // Ключ складывался из врача, назначения, суммы и числа месяца, и у
    // двух РАЗНЫХ приходов совпадал. Врач, купивший второй месяц в тот
    // же день, получал «уже оформлен» и второго месяца не получал, а
    // деньги за него были приняты.
    const ключи: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST' && path.includes('/payments')) {
          ключи.push(JSON.parse(String(init.body)).idemKey)
          return Promise.resolve(
            new Response(JSON.stringify({ id: ключи.length, kopecks: 199000 }), {
              status: 200,
            }),
          )
        }
        const found = ОБЫЧНО.find(([key]) => path.startsWith(key))
        return Promise.resolve(
          new Response(JSON.stringify(found ? found[1] : {}), { status: 200 }),
        )
      }),
    )
    render(<Sales me={ОПЕРАТОР} />)
    fireEvent.click(await screen.findByText('Иванов И.И.'))
    for (const _ of [1, 2]) {
      const поле = await screen.findByPlaceholderText('1990')
      fireEvent.change(поле, { target: { value: '1990' } })
      fireEvent.click(screen.getByRole('button', { name: 'Оформить приход' }))
      await waitFor(() => expect(screen.getByText(/Приход оформлен/)).toBeTruthy())
    }
    expect(ключи).toHaveLength(2)
    expect(ключи[0]).not.toBe(ключи[1])
  })

  it('закрытый вход назван закрытым, и предлагается его открыть', async () => {
    serve([['/admin/api/clients/7/payments', { payments: [] }],
           ['/admin/api/clients/7/entitlements', { entitlements: [] }],
           ['/admin/api/clients/7', KLIENT(true)],
           ['/admin/api/clients', { clients: [KLIENT(true)] }],
           ['/admin/api/prices', { prices: [] }],
           ['/admin/api/packs', { packs: [] }]])
    render(<Sales me={ОПЕРАТОР} />)
    expect(await screen.findByText('вход закрыт')).toBeTruthy()
    fireEvent.click(screen.getByText('Иванов И.И.'))
    expect(await screen.findByText('Открыть вход')).toBeTruthy()
  })

  it('врач без имени и почты назван номером один раз, а не дважды', async () => {
    // «№ 6 · № 6» читается как сбой, а не как сведения: запись заводится
    // молча, при первом запуске, и ни имени, ни почты у неё нет.
    const безымянный = { ...КЛИЕНТЫ.clients[0]!, displayName: '', email: '' }
    serve([
      ['/admin/api/clients', { clients: [безымянный] }],
      ['/admin/api/prices', { prices: [] }],
      ['/admin/api/packs', { packs: [] }],
    ])
    render(<Sales me={ОПЕРАТОР} />)
    const строка = (await screen.findByText('Врач № 7')).closest('.list-row')
    expect(строка?.textContent).toBe('Врач № 7прав: 1')
  })

  it('отказ по сумме стоит под полем суммы, а не полосой наверху', async () => {
    // Полоса наверху страницы говорила «Сумма пишется рублями и
    // копейками» над формой из трёх полей и не говорила, о котором из
    // них речь: оператор перебирал их вслепую.
    render(<Sales me={ОПЕРАТОР} />)
    const поле = await screen.findByLabelText('Сколько, рублей')
    fireEvent.change(поле, { target: { value: 'тысяча' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить цену' }))

    const отказ = await screen.findByRole('alert')
    expect(отказ.className).toContain('fld-error')
    // Поле объявлено негодным и связано с отказом: читающий с экрана
    // услышит его, встав на поле, а не только в момент отказа.
    expect(поле.getAttribute('aria-invalid')).toBe('true')
    expect(поле.getAttribute('aria-describedby')).toBe(отказ.id)
    // И отказ стоит ВНУТРИ строки этого поля, а не где-то на странице.
    expect(поле.closest('.form-row')?.contains(отказ)).toBe(true)
  })

  it('правка суммы убирает прежний отказ', async () => {
    // Отказ, переживший исправление, читается как отказ на исправленное.
    render(<Sales me={ОПЕРАТОР} />)
    const поле = await screen.findByLabelText('Сколько, рублей')
    fireEvent.change(поле, { target: { value: 'тысяча' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить цену' }))
    await screen.findByRole('alert')

    fireEvent.change(поле, { target: { value: '1990' } })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('пустая витрина сказана словами, а не пустым местом', async () => {
    serve([['/admin/api/clients', { clients: [] }],
           ['/admin/api/prices', { prices: [] }],
           ['/admin/api/packs', { packs: [] }]])
    render(<Sales me={ОПЕРАТОР} />)
    expect(await screen.findByText(/витрина ничего не предлагает/)).toBeTruthy()
  })
})
