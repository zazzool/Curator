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

const ПОЛЬЗОВАТЕЛИ = {
  users: [
    {
      login: 'мастер',
      displayName: 'Мастер',
      permissions: ['prompts', 'workshop'],
      disabled: false,
      createdAt: '2026-09-01T10:00:00Z',
    },
    {
      login: 'всевластный',
      displayName: 'Владелец',
      permissions: [
        'source:read', 'source:accept', 'case:read', 'case:write',
        'generate', 'prompts', 'packs', 'clients', 'sales', 'analytics', 'workshop',
      ],
      disabled: false,
      createdAt: '2026-07-01T10:00:00Z',
    },
    {
      login: 'уволенный',
      displayName: 'Бывший составитель',
      permissions: [],
      disabled: true,
      createdAt: '2026-08-01T10:00:00Z',
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

// Мастерская ходит за тремя списками сразу, и подставлять их надо все
// три: экран, которому не ответили, остаётся в «Читаем…» и молча уводит
// проверку от того, что она проверяет.
function ответ(path: string, prompts: unknown, keys: unknown, users: unknown) {
  if (path.startsWith('/admin/api/prompts')) return prompts
  if (path.startsWith('/admin/api/users')) return users
  return keys
}

function serve(prompts: unknown, keys: unknown, users: unknown = ПОЛЬЗОВАТЕЛИ) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) =>
      Promise.resolve(
        new Response(JSON.stringify(ответ(path, prompts, keys, users)), { status: 200 }),
      ),
    ),
  )
}

describe('мастерская', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
    serve(ЗАДАНИЯ, КЛЮЧИ)
  })

  it('задание открывается на правку с нынешней редакцией', async () => {
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('Ты врач-методист.')).toBeTruthy()
    expect(screen.getByDisplayValue('Напиши задачу по {{unit}}.')).toBeTruthy()
  })

  it('набранное переживает закрытие вкладки', async () => {
    // Токен студии живёт только в памяти страницы, поэтому F5 по
    // привычке, уснувший ноутбук и истёкшая сессия — одно и то же
    // событие. Методист переписывает задание сорок минут; терять это
    // из-за нажатия Ctrl-R нельзя.
    const { unmount } = render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    const поле = await screen.findByDisplayValue('Ты врач-методист.')
    fireEvent.change(поле, { target: { value: 'Ты врач-методист. Пиши строго.' } })
    unmount()

    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('Ты врач-методист. Пиши строго.')).toBeTruthy()
    expect(screen.getByText(/Восстановлено несохранённое/)).toBeTruthy()
  })

  it('черновик от прежней редакции не подставляется молча', async () => {
    // Задание с тех пор правил кто-то другой. Подставь черновик — и
    // человек допишет поверх чужой работы, не увидев её; сохранить это
    // всё равно не дала бы сверка редакций на сервере.
    const { unmount } = render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    const поле = await screen.findByDisplayValue('Ты врач-методист.')
    fireEvent.change(поле, { target: { value: 'моё несохранённое' } })
    unmount()

    const ушедшее = {
      prompts: [{ ...ЗАДАНИЯ.prompts[0]!, systemMd: 'чужая правка', revision: 5 }],
    }
    serve(ушедшее, КЛЮЧИ)
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('чужая правка')).toBeTruthy()
    expect(screen.queryByText(/Восстановлено несохранённое/)).toBeNull()
  })

  it('сохранённое задание не возвращается как несохранённое', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          return Promise.resolve(new Response(JSON.stringify({ revision: 5 }), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ)), {
            status: 200,
          }),
        )
      }),
    )
    const { unmount } = render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    const поле = await screen.findByDisplayValue('Ты врач-методист.')
    fireEvent.change(поле, { target: { value: 'выверено и сохранено' } })
    fireEvent.click(screen.getByText('Сохранить задание'))
    await waitFor(() => expect(screen.getByText(/Задание сохранено/)).toBeTruthy())
    unmount()

    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('Ты врач-методист.')).toBeTruthy()
    expect(screen.queryByText(/Восстановлено несохранённое/)).toBeNull()
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ)), {
            status: 200,
          }),
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ)), {
            status: 200,
          }),
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
    expect(screen.queryByText('Завести вход')).toBeNull()
    expect(screen.getByText(/кому выдано право «задания»/)).toBeTruthy()
    // Оба раздела мастерской говорят, кто держит право: скрытое действие
    // без объяснения выглядит поломкой, а не запретом.
    expect(screen.getAllByText(/кому выдано право «мастерская»/)).toHaveLength(2)
  })

  it('список пользователей показывает права словами и закрытый вход', async () => {
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('Бывший составитель')).toBeTruthy()
    // Права показаны словами: код «prompts» ничего не говорит тому, кто
    // решает, что выдать человеку.
    expect(screen.getByText(/задания моделям, мастерская/)).toBeTruthy()
    expect(screen.getByText('вход закрыт')).toBeTruthy()
    expect(screen.getByText(/прав нет: войти сможет, а разделов не увидит/)).toBeTruthy()
    // Полный набор сворачивается: перечисленный целиком, он одинаков у
    // каждого такого человека, и список превращается в стену, по которой
    // не видно, чем люди отличаются.
    expect(screen.getByText('все права')).toBeTruthy()
  })

  it('секрет аутентификатора показывается целиком и один раз', async () => {
    // Он хранится, чтобы сверять коды, а не чтобы его смотреть: не показав
    // сейчас, мы не покажем никогда, и вход придётся заводить заново.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                login: 'ivanova',
                secret: 'СЕКРЕТ-входа',
                note: 'Передайте секрет человеку сейчас',
              }),
              { status: 201 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.change(await screen.findByPlaceholderText('ivanova'), {
      target: { value: 'ivanova' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Завести вход' }))
    await waitFor(() => expect(screen.getByText('СЕКРЕТ-входа')).toBeTruthy())
  })

  it('отказ за последнего мастера показывается словами сервера', async () => {
    // Это не негодный запрос, а верный запрос в негодный момент, и сервер
    // говорит, что делать. Своё «не удалось» отправило бы человека
    // нажимать ту же кнопку снова.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                error:
                  'Это последний человек с правом мастерской: сняв его, ' +
                  'завести пользователя будет некому',
              }),
              { status: 409 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    await screen.findByText('Бывший составитель')
    fireEvent.click(screen.getAllByRole('button', { name: 'Закрыть вход' })[0]!)
    expect(await screen.findByText(/последний человек с правом мастерской/)).toBeTruthy()
  })
})
