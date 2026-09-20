import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'

const Я = { login: 'мастер', displayName: 'Мастер', permissions: ['source:read'] }

/**
 * Дверь студии, отвечающая по пути.
 *
 * `вошёл` решает, что ответит «кто я»: 200 — печенье сессии живо и студия
 * обязана открыться сама; 401 — печенья нет, и человек видит вход.
 */
function дверь(вошёл: boolean) {
  return vi.fn((path: string) => {
    if (path === '/admin/api/me') {
      return Promise.resolve(
        вошёл
          ? new Response(JSON.stringify(Я), { status: 200 })
          : new Response(JSON.stringify({ error: 'Войдите в студию заново' }), { status: 401 }),
      )
    }
    // Всё прочее, что спрашивает открытая студия, — пустыми списками:
    // предмет проверки здесь вход, а не наполнение разделов.
    return Promise.resolve(new Response(JSON.stringify({ sources: [] }), { status: 200 }))
  })
}

describe('возврат к открытой сессии', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('открытая сессия не спрашивает кода заново', async () => {
    // Это и есть перезагрузка страницы: токена в памяти нет, печенье есть.
    // До него F5 выбрасывал на вход при какой угодно сессии на сервере.
    vi.stubGlobal('fetch', дверь(true))
    render(<App />)

    expect(await screen.findByText('Мастер')).toBeTruthy()
    expect(screen.queryByLabelText('Код')).toBeNull()
  })

  it('без сессии показывается вход, и он не говорит, что человек вышел', async () => {
    // «Сессия кончилась» на первом же заходе — неправда: человек не выходил,
    // он ещё не входил.
    vi.stubGlobal('fetch', дверь(false))
    render(<App />)

    expect(await screen.findByLabelText('Код')).toBeTruthy()
    expect(screen.queryByText(/Сессия кончилась/)).toBeNull()
  })

  it('форма входа не мелькает, пока идёт вопрос о сессии', async () => {
    // Вопрос уходит к своему же серверу и укладывается в мгновение, но
    // мелькнувшая форма — это вошедший, начавший набирать имя.
    vi.stubGlobal('fetch', дверь(true))
    const { container } = render(<App />)

    expect(container.querySelector('.gate')).toBeNull()
    await waitFor(() => expect(screen.getByText('Мастер')).toBeTruthy())
  })
})
