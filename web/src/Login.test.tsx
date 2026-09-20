import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Login } from './Login'

const Я = { login: 'мастер', displayName: 'Мастер', permissions: ['workshop'] }

// Дверь отвечает на два обращения: вход и «кто я». Отдельная заглушка на
// каждое, а не одна на всё, — иначе проверка отказа не отличит, на каком из
// двух он случился.
function дверь(вход: () => Response) {
  return vi.fn((path: string) =>
    Promise.resolve(
      path === '/admin/api/login'
        ? вход()
        : new Response(JSON.stringify(Я), { status: 200 }),
    ),
  )
}

const пускает = () => new Response(JSON.stringify({ token: 'токен' }), { status: 200 })
const отвергает = () =>
  new Response(JSON.stringify({ error: 'Имя или код не подошли' }), { status: 401 })

function поле(подпись: string): HTMLInputElement {
  return screen.getByLabelText(подпись) as HTMLInputElement
}

describe('вход в студию', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('не рассказывает, как устроен вход', () => {
    // Пояснение про программу-аутентификатор и про то, что код живёт
    // полминуты, называло постороннему, что именно красть и в каком окне
    // укладываться. Человек, у которого аутентификатор есть, знает это и
    // без страницы.
    render(<Login onEnter={() => undefined} />)

    const текст = (document.body.textContent ?? '').toLowerCase()
    for (const лишнее of ['аутентификатор', 'полминуты', 'следующего', 'одноразов']) {
      expect(текст).not.toContain(лишнее)
    }
  })

  it('длина кода сказана только читающему с экрана', () => {
    // Граница проходит между формой ввода и устройством входа. Сколько
    // знаков в коде, видно и по самому полю — оно заполняется на глазах, —
    // а читающему с экрана видеть нечего, и фразу он получает. Откуда код
    // берётся и сколько живёт, не сказано никому.
    render(<Login onEnter={() => undefined} />)

    const мера = document.getElementById('gate-code-shape')
    expect(мера?.textContent?.trim()).toBe('Шесть цифр')
    expect(мера?.className).toBe('visually-hidden')
    expect(поле('Код').getAttribute('aria-describedby')).toBe('gate-code-shape')
  })

  it('набранный до конца код уходит без нажатия', async () => {
    const fetchStub = дверь(пускает)
    vi.stubGlobal('fetch', fetchStub)
    const вошёл = vi.fn()
    render(<Login onEnter={вошёл} />)

    fireEvent.change(поле('Имя'), { target: { value: 'мастер' } })
    fireEvent.change(поле('Код'), { target: { value: '123456' } })

    await waitFor(() => expect(вошёл).toHaveBeenCalled())
    expect(fetchStub.mock.calls[0]?.[0]).toBe('/admin/api/login')
  })

  it('из кода выбрасываются нецифры и всё сверх шести знаков', () => {
    // Из аутентификатора код копируют вместе с пробелом посередине, а поле
    // с мусором отправило бы серверу заведомо негодную попытку — то есть
    // сожгло бы одну из считаных.
    render(<Login onEnter={() => undefined} />)

    fireEvent.change(поле('Код'), { target: { value: '12 34567' } })
    expect(поле('Код').value).toBe('123456')
  })

  it('после отказа имя остаётся, а код стирается', async () => {
    vi.stubGlobal('fetch', дверь(отвергает))
    render(<Login onEnter={() => undefined} />)

    fireEvent.change(поле('Имя'), { target: { value: 'мастер' } })
    fireEvent.change(поле('Код'), { target: { value: '000000' } })

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(поле('Имя').value).toBe('мастер')
    expect(поле('Код').value).toBe('')
    // Курсор возвращается в поле кода: исправлять человеку нужно код, а не
    // имя, и лишнее нажатие здесь стоит ещё одной попытки.
    //
    // Ожиданием, а не сразу: курсор переставляет useEffect, а он идёт
    // ПОСЛЕ отрисовки. Между появлением отказа на экране и переездом
    // курсора есть промежуток, и под нагрузкой он растягивается: у себя
    // проверка проходила всегда, у сторожа упала. Проверять надо то, что
    // увидит человек, а увидит он уже переехавший курсор.
    await vi.waitFor(() => {
      expect(document.activeElement).toBe(поле('Код'))
    })
  })

  it('отказ приходит словами сервера и не различает случаи', async () => {
    vi.stubGlobal('fetch', дверь(отвергает))
    render(<Login onEnter={() => undefined} />)

    fireEvent.change(поле('Имя'), { target: { value: 'кого-нет' } })
    fireEvent.change(поле('Код'), { target: { value: '000000' } })

    const отказ = await screen.findByRole('alert')
    expect(отказ.textContent).toBe('Имя или код не подошли')
  })

  it('кнопка недоступна, пока форма не заполнена', () => {
    // Каждое пустое нажатие — ещё одна неудачная попытка, после которой
    // сервер придержит следующий ответ.
    render(<Login onEnter={() => undefined} />)
    const войти = screen.getByRole('button', { name: 'Войти' }) as HTMLButtonElement

    expect(войти.disabled).toBe(true)
    fireEvent.change(поле('Имя'), { target: { value: 'мастер' } })
    expect(войти.disabled).toBe(true)
    fireEvent.change(поле('Код'), { target: { value: '12345' } })
    expect(войти.disabled).toBe(true)
  })
})
