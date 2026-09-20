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
  ['/admin/api/prices', { prices: [{ purpose: 'subscription:month', kopecks: 199000, enabled: true }] }],
  ['/admin/api/packs', { packs: [] }],
]

describe('продажи', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve(ОБЫЧНО)
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
    await waitFor(() => expect(screen.getByText(/Сумма пишется рублями/)).toBeTruthy())
    expect(posted).toBe(0)
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

  it('пустая витрина сказана словами, а не пустым местом', async () => {
    serve([['/admin/api/clients', { clients: [] }],
           ['/admin/api/prices', { prices: [] }],
           ['/admin/api/packs', { packs: [] }]])
    render(<Sales me={ОПЕРАТОР} />)
    expect(await screen.findByText(/витрина ничего не предлагает/)).toBeTruthy()
  })
})
