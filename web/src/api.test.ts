import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, api, setToken } from './api'

function answer(status: number, body: unknown) {
  return Promise.resolve(
    new Response(typeof body === 'string' ? body : JSON.stringify(body), { status }),
  )
}

describe('обращение к редакционному API', () => {
  beforeEach(() => {
    setToken('')
    vi.restoreAllMocks()
  })

  it('показывает человеку текст отказа, а не свой', async () => {
    // Сервер пишет отказ по-русски и говорит, чего не хватает. Замени его
    // здесь на «что-то пошло не так» — и человек пойдёт перебирать поля
    // вслепую.
    vi.stubGlobal('fetch', vi.fn(() => answer(400, { error: 'У источника не названа ось' })))
    await expect(api.sources()).rejects.toThrow('У источника не названа ось')
  })

  it('не показывает кусок HTML, когда до сервера не дошли', async () => {
    vi.stubGlobal('fetch', vi.fn(() => answer(502, '<html>прокси</html>')))
    await expect(api.sources()).rejects.toBeInstanceOf(ApiError)
    await expect(api.sources()).rejects.toThrow('обновите страницу')
  })

  it('носит токен в каждом обращении', async () => {
    const fetcher = vi.fn((_path: string, _init?: RequestInit) => answer(200, { sources: [] }))
    vi.stubGlobal('fetch', fetcher)
    setToken('токен')
    await api.sources()
    const init = fetcher.mock.calls[0]![1]!
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer токен')
  })

  it('срез по пути уезжает запросом, а не отбором на странице', async () => {
    // Отбор на странице означал бы, что студия сперва поднимает весь
    // справочник в тысячу единиц. Срез берёт сервер.
    const fetcher = vi.fn((_path: string, _init?: RequestInit) => answer(200, { units: [] }))
    vi.stubGlobal('fetch', fetcher)
    await api.units(7, 'F3')
    expect(fetcher.mock.calls[0]![0]).toBe('/admin/api/sources/7/units?path=F3')
  })

  it('файл уезжает формой, а не строкой', async () => {
    const fetcher = vi.fn((_path: string, _init?: RequestInit) =>
      answer(201, { id: 1, format: 'markdown', fragments: 2 }),
    )
    vi.stubGlobal('fetch', fetcher)
    await api.upload(3, new File(['# Раз'], 'приказ.md', { type: 'text/markdown' }))
    const init = fetcher.mock.calls[0]![1]!
    expect(init.body).toBeInstanceOf(FormData)
    // Заголовок не ставится руками: границу частей формы дописывает
    // браузер, и заданный вручную Content-Type её теряет — сервер получает
    // форму, которую не может разобрать.
    expect((init.headers as Record<string, string>)['Content-Type']).toBeUndefined()
  })
})
