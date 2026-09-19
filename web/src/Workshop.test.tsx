import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Workshop } from './Workshop'
import type { Me } from './api'

const МАСТЕР: Me = {
  login: 'мастер',
  displayName: 'Мастер',
  permissions: ['prompts', 'workshop'],
}
const РЕДАКТОР: Me = { login: 'редактор', displayName: 'Редактор', permissions: ['case:read'] }

const ЗАДАНИЯ = {
  prompts: [
    {
      id: 'draft',
      name: 'Черновик задачи',
      node: 'draft',
      nodeWord: 'черновик',
      systemMd: 'Ты врач-методист.',
      userMd: 'Напиши задачу по {{unit}}.',
      revision: 4,
    },
  ],
}

const КЛЮЧИ = {
  keys: [
    {
      keyId: 'android-2026-09',
      title: 'сборка для Android',
      disabled: false,
      createdAt: '2026-09-01T10:00:00Z',
    },
  ],
}

function serve(prompts: unknown, keys: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) =>
      Promise.resolve(
        new Response(JSON.stringify(path.startsWith('/admin/api/prompts') ? prompts : keys), {
          status: 200,
        }),
      ),
    ),
  )
}

describe('мастерская', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    serve(ЗАДАНИЯ, КЛЮЧИ)
  })

  it('задание открывается на правку с нынешней редакцией', async () => {
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('Ты врач-методист.')).toBeTruthy()
    expect(screen.getByDisplayValue('Напиши задачу по {{unit}}.')).toBeTruthy()
  })

  it('отказ по чужой правке показывается словами сервера', async () => {
    // Редакция сверяется на сервере, и его отказ говорит, что произошло.
    // Своё «не удалось сохранить» отправило бы человека нажимать ту же
    // кнопку снова — поверх чужой правки.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          return Promise.resolve(
            new Response(
              JSON.stringify({ error: 'Задание уже правил кто-то другой, перечитайте его' }),
              { status: 409 },
            ),
          )
        }
        return Promise.resolve(
          new Response(
            JSON.stringify(path.startsWith('/admin/api/prompts') ? ЗАДАНИЯ : КЛЮЧИ),
            { status: 200 },
          ),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    fireEvent.click(await screen.findByText('Сохранить задание'))
    expect(await screen.findByText(/уже правил кто-то другой/)).toBeTruthy()
  })

  it('заведённый ключ показывается целиком и один раз', async () => {
    // В базе лежит только отпечаток: не показав ключ сейчас, мы не покажем
    // его никогда, и сборку придётся заводить заново.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          return Promise.resolve(
            new Response(
              JSON.stringify({ key: 'СЕКРЕТ-ключа', note: 'Сохраните ключ сейчас' }),
              { status: 201 },
            ),
          )
        }
        return Promise.resolve(
          new Response(
            JSON.stringify(path.startsWith('/admin/api/prompts') ? ЗАДАНИЯ : КЛЮЧИ),
            { status: 200 },
          ),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.change(await screen.findByPlaceholderText('android-2026-09'), {
      target: { value: 'android-2026-10' },
    })
    fireEvent.click(screen.getByText('Завести ключ'))
    await waitFor(() => expect(screen.getByText('СЕКРЕТ-ключа')).toBeTruthy())
  })

  it('без ключей сказано, что ни одна сборка не подключится', async () => {
    serve({ prompts: [] }, { keys: [] })
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText(/ни одна сборка приложения к серверу не подключится/))
      .toBeTruthy()
    expect(screen.getByText(/Заданий нет: генерация не запустится/)).toBeTruthy()
  })

  it('без прав разделы открыты, а действий нет', async () => {
    render(<Workshop me={РЕДАКТОР} />)
    await screen.findByText('Черновик задачи')
    expect(screen.queryByText('Завести ключ')).toBeNull()
    expect(screen.getByText(/кому выдано право «задания»/)).toBeTruthy()
    expect(screen.getByText(/кому выдано право «мастерская»/)).toBeTruthy()
  })
})
