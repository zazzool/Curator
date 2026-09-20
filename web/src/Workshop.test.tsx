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
      model: '',
      revision: 4,
    },
  ],
}

const КОНВЕЙЕР = {
  days: 30,
  source: 0,
  nodes: [
    {
      node: 'compose', word: 'написание', promptId: 'compose-default',
      promptName: 'Написание условия', model: 'дорогая/рассуждающая',
      calls: 40, failed: 2, medianNanoUsd: 3_200_000, medianMs: 4200, estimated: 38, own: false,
    },
    {
      node: 'verify', word: 'слепая сверка', promptId: 'verify-default',
      promptName: 'Слепая сверка', model: '',
      calls: 0, failed: 0, medianNanoUsd: 0, medianMs: 0, estimated: 0, own: false,
    },
    {
      node: 'siblings', word: 'различающая сверка', promptId: '',
      promptName: '', model: 'дешёвая/быстрая',
      calls: 6, failed: 6, medianNanoUsd: 0, medianMs: 0, estimated: 0, own: false,
    },
  ],
}

const ИСТОЧНИКИ = {
  sources: [
    {
      id: 7, slug: 'prikaz', kind: 'decree', title: 'Приказ о сроках',
      unitWord: 'пункт', statementWord: 'указание', purpose: 'legal',
      hierarchy: 'part-of', completeness: 'fragment', edition: '', status: 'ready',
    },
  ],
}

const МОДЕЛИ = {
  models: [
    { provider: 'openrouter', model: 'дорогая/рассуждающая', promptNanoUsd: 3000, completionNanoUsd: 15000 },
    { provider: 'openrouter', model: 'дешёвая/быстрая', promptNanoUsd: 100, completionNanoUsd: 400 },
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

const СВОД = {
  rules: [
    {
      id: 'builtin:no-label',
      title: 'Метки единицы в условии не бывает',
      text: 'Не пиши в условии метку единицы источника.',
      why: 'Метка снимает задачу целиком, а выглядит добросовестной ссылкой.',
      kind: 'structure',
      kindWord: 'устройство',
      source: 'builtin',
      status: 'active',
      pinned: false,
      scope: { nodes: ['compose'] },
      checkWords: '',
      confirmations: 5,
      seenJobs: [1, 2, 3, 4, 5],
      quorum: 3,
      validFrom: '2026-09-01T10:00:00Z',
      updatedAt: '2026-09-20T10:00:00Z',
    },
    {
      id: 'lint:term:7:риту',
      title: 'Слово «ритуально» в условии',
      text: 'Не пиши в условии слово «ритуально» и однокоренные.',
      why: 'Слово протекало в условия по источнику «Приказ № 1130н».',
      kind: 'substance',
      kindWord: 'существо',
      source: 'lint',
      status: 'candidate',
      pinned: false,
      scope: { sources: [7], nodes: ['compose'] },
      check: { type: 'forbid-words', words: ['ритуально'], where: ['segments'] },
      checkWords: 'в фрагментах условия не должно быть: ритуально',
      confirmations: 2,
      seenJobs: [11, 12],
      quorum: 3,
      validFrom: '2026-09-19T10:00:00Z',
      updatedAt: '2026-09-20T10:00:00Z',
    },
  ],
}

// Каталог предикатов: он закрыт и живёт на сервере, и форма строится по
// нему. Повтори его студия — разошлись бы они молча на том предикате,
// который добавили последним.
const КАТАЛОГ_ПРОВЕРОК = {
  checks: [
    {
      type: 'forbid-words', title: 'Запрещённые слова',
      about: 'слов быть не должно',
      params: ['words', 'where', 'gender', 'allowNegated'],
    },
    {
      type: 'length', title: 'Длина текста',
      about: 'сколько знаков должно быть в условии, заголовке или разборе',
      params: ['where', 'min', 'max'],
    },
    {
      type: 'pattern', title: 'Выражение',
      about: 'регулярное выражение; пишет только составитель',
      params: ['pattern', 'where'], doctorOnly: true,
    },
  ],
  where: [
    { value: 'segments', word: 'фрагменты условия' },
    { value: 'title', word: 'заголовок' },
    { value: 'explanation', word: 'разбор' },
  ],
  what: [
    { value: 'segments', word: 'фрагментов условия' },
    { value: 'options', word: 'вариантов ответа' },
  ],
  gender: [
    { value: 'm', word: 'условие о мужчине' },
    { value: 'f', word: 'условие о женщине' },
  ],
}

// Мастерская ходит за четырьмя списками сразу, и подставлять их надо все
// четыре: экран, которому не ответили, остаётся в «Читаем…» и молча
// уводит проверку от того, что она проверяет.
function ответ(path: string, prompts: unknown, keys: unknown, users: unknown, rules: unknown) {
  if (path.startsWith('/admin/api/prompts')) return prompts
  if (path.startsWith('/admin/api/users')) return users
  // Каталог проверок стоит ПЕРЕД сводом: его путь начинается тем же
  // «/admin/api/rules», и свод перехватил бы его себе.
  if (path.startsWith('/admin/api/rules/checks')) return КАТАЛОГ_ПРОВЕРОК
  if (path.startsWith('/admin/api/rules')) return rules
  // Список моделей — пятый, и подставляется он здесь по тому же доводу,
  // что и остальные четыре: экран, которому не ответили, остаётся в
  // «Читаем…» и молча уводит проверку от её предмета.
  if (path.startsWith('/admin/api/models')) return МОДЕЛИ
  if (path.startsWith('/admin/api/pipeline')) return КОНВЕЙЕР
  if (path.startsWith('/admin/api/sources')) return ИСТОЧНИКИ
  return keys
}

function serve(
  prompts: unknown,
  keys: unknown,
  users: unknown = ПОЛЬЗОВАТЕЛИ,
  rules: unknown = СВОД,
) {
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string) =>
      Promise.resolve(
        new Response(JSON.stringify(ответ(path, prompts, keys, users, rules)), { status: 200 }),
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

  it('на отказе показывает отказ и НЕ оставляет вечное «Читаем…»', async () => {
    // Составитель без права «мастерская» открывал раздел, получал внятный
    // отказ по-русски и под ним вечное «Читаем…»: студия говорила ему, что
    // грузится то, чего не будет никогда. Списки заводились значением
    // «ещё не прочитано», и отказ его не менял.
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ error: 'Этот раздел вам не открыт' }), {
            status: 403,
          }),
        ),
      ),
    )
    render(<Workshop me={РЕДАКТОР} />)
    await screen.findAllByText('Этот раздел вам не открыт')
    // Ждём отдельно: «Читаем…» гаснет тем же состоянием, что зажигает
    // отказ, но сверка сразу после появления отказа успевала бы застать
    // предыдущую отрисовку.
    await waitFor(() => {
      expect(screen.queryByText('Читаем…')).toBeNull()
    })
  })

  it('задание открывается на правку с нынешней редакцией', async () => {
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('Ты врач-методист.')).toBeTruthy()
    expect(screen.getByDisplayValue('Напиши задачу по {{unit}}.')).toBeTruthy()
  })

  it('сохранение задания шлёт нынешнюю редакцию', async () => {
    // Прежняя проверка сверяла только то, что поля открылись на правку:
    // убери `revision` из запроса — и она осталась бы зелёной, а
    // взаимная перезапись вернулась бы молча. Сверка редакции — то
    // единственное, что стоит между двумя методистами и потерянной
    // работой одного из них.
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    await screen.findByDisplayValue('Ты врач-методист.')
    fireEvent.click(screen.getByText('Сохранить задание'))

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) =>
          String(call[0]).includes('/admin/api/prompts/draft') &&
          (call[1] as { method?: string })?.method === 'PUT',
      )
      expect(sent).toBeTruthy()
      const body = JSON.parse(String((sent![1] as { body?: string }).body))
      expect(body.revision).toBe(4)
    })
  })

  it('конвейер показан целиком и в порядке прохождения', async () => {
    // Перечень узлов приходит с сервера и в студии не повторяется:
    // повторённый разошёлся бы молча на узле, который добавили
    // последним, и экран показал бы конвейер короче настоящего — не
    // поломкой, а конвейером покороче.
    render(<Workshop me={МАСТЕР} />)
    const заголовки = await screen.findAllByText(/написание|слепая сверка|различающая сверка/)
    expect(заголовки.length).toBeGreaterThanOrEqual(3)
    // Порядок — сведение, а не вёрстка: вычитка стоит между написанием и
    // сверками потому, что сверки обязаны мерить то, что уйдёт
    // обучающемуся.
    const текст = заголовки.map((one) => one.textContent ?? '').join('|')
    expect(текст.indexOf('написание')).toBeLessThan(текст.indexOf('слепая сверка'))
  })

  it('цена узла показана медианой и помечена оценкой', async () => {
    // Оценка не выдаётся за факт: расчёт по прайсу не знает ни скидки
    // префиксного кэша, ни наценки шлюза и ошибается в разы там, где кэш
    // работает. Число, которое выглядит точным и таковым не является,
    // хуже отсутствия числа.
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('$0,0032')).toBeTruthy()
    expect(screen.getByText(/оценка у 38 из 38/)).toBeTruthy()
  })

  it('узел без состоявшихся обращений не выглядит даровым', async () => {
    // «$0» читается как «бесплатный узел», и решали бы по нему не то.
    // Случая два, и они разные для узла, хотя одинаковые для цены: через
    // узел не проходило ничего — и через узел проходило, но не
    // состоялось ни разу. Второй прятался за нулём особенно охотно:
    // отказ чаще всего не стоит ничего.
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('обращений не было')).toBeTruthy()
    expect(screen.getByText('ни одно не состоялось')).toBeTruthy()
    expect(screen.queryByText('$0')).toBeNull()
  })

  it('узел без задания назван, а не пропущен', async () => {
    // Задания нет — узел не работает вовсе. Пропусти студия строку, и
    // пропажа задания выглядела бы как пропажа узла, а искать её пошли
    // бы в коде.
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('задания нет')).toBeTruthy()
    expect(screen.getByText(/различающая сверка/)).toBeTruthy()
  })

  it('конвейер не показывается тому, кому не выдано право «задания»', async () => {
    render(<Workshop me={РЕДАКТОР} />)
    await waitFor(() => {
      expect(screen.queryByText('Конвейер')).toBeNull()
    })
  })

  it('конвейер открывается на общем, а не на чьём-то', async () => {
    // Общим работают все источники, кроме тех, кому завели своё. Открой
    // экран на чьём-то конвейере, и составитель правил бы задание одного
    // источника, думая, что правит общее, — а узнал бы об этом по
    // качеству чужих задач.
    render(<Workshop me={МАСТЕР} />)
    const выбор = (await screen.findByLabelText('Чей конвейер')) as HTMLSelectElement
    expect(выбор.value).toBe('0')
    // Кнопок «Завести своё» на общем нет: своё заводится источнику, а
    // не никому.
    expect(screen.queryByText('Завести своё')).toBeNull()
  })

  it('на конвейере источника видно, где своё задание, а где общее', async () => {
    serve(ЗАДАНИЯ, КЛЮЧИ)
    render(<Workshop me={МАСТЕР} />)
    const выбор = await screen.findByLabelText('Чей конвейер')
    // Список источников — с сервера: вторая таблица «какие бывают
    // источники» разошлась бы с первой молча.
    expect(screen.getByText('Приказ о сроках')).toBeTruthy()

    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        if (String(path).startsWith('/admin/api/pipeline?source=7')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                ...КОНВЕЙЕР,
                source: 7,
                nodes: [{ ...КОНВЕЙЕР.nodes[0]!, own: true }, ...КОНВЕЙЕР.nodes.slice(1)],
              }),
              { status: 200 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    fireEvent.change(выбор, { target: { value: '7' } })

    // Кнопка названа тем, что она сделает, а не тем, что уже есть:
    // подписанная состоянием («своё»), она читается как «уже своё», и
    // нажимают её именно те, кто хотел вернуть общее.
    expect(await screen.findByText('Вернуть общее')).toBeTruthy()
    expect(screen.getAllByText('Завести своё').length).toBe(КОНВЕЙЕР.nodes.length - 1)
  })

  it('заведение своего задания шлёт узел и источник', async () => {
    render(<Workshop me={МАСТЕР} />)
    const выбор = await screen.findByLabelText('Чей конвейер')
    fireEvent.change(выбор, { target: { value: '7' } })
    fireEvent.click((await screen.findAllByText('Завести своё'))[0]!)

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) => (call[1] as { method?: string })?.method === 'POST',
      )
      expect(sent).toBeTruthy()
      expect(String(sent![0])).toBe('/admin/api/sources/7/pipeline/compose')
    })
  })

  it('возврат к общему шлёт снятие, а не заведение', async () => {
    // Снимается только привязка, и путать её с заведением нельзя: одно
    // нажатие вместо другого завело бы копию там, где составитель
    // возвращался к общему.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        const метод = (init as { method?: string })?.method
        if (метод === 'POST' || метод === 'DELETE') {
          return Promise.resolve(new Response(JSON.stringify({ status: 'общее' }), { status: 200 }))
        }
        if (String(path).startsWith('/admin/api/pipeline?source=7')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                ...КОНВЕЙЕР,
                source: 7,
                nodes: [{ ...КОНВЕЙЕР.nodes[0]!, own: true }],
              }),
              { status: 200 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    const выбор = await screen.findByLabelText('Чей конвейер')
    fireEvent.change(выбор, { target: { value: '7' } })
    fireEvent.click(await screen.findByText('Вернуть общее'))

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) => (call[1] as { method?: string })?.method === 'DELETE',
      )
      expect(sent).toBeTruthy()
      expect(String(sent![0])).toBe('/admin/api/sources/7/pipeline/compose')
    })
  })

  it('заведение своего задания перечитывает экран', async () => {
    // Без перечитывания строка остаётся прежней: кнопка по-прежнему
    // зовёт завести своё, и пометки «своё» нет. Составитель жмёт второй
    // раз, а задание уже заведено — то есть экран показывает не то, что
    // в базе, и молчит об этом.
    let завели = false
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if ((init as { method?: string })?.method === 'POST') {
          завели = true
          return Promise.resolve(
            new Response(JSON.stringify({ promptId: 'compose-источник-7' }), { status: 200 }),
          )
        }
        if (String(path).startsWith('/admin/api/pipeline?source=7')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                ...КОНВЕЙЕР,
                source: 7,
                nodes: [{ ...КОНВЕЙЕР.nodes[0]!, own: завели }],
              }),
              { status: 200 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.change(await screen.findByLabelText('Чей конвейер'), { target: { value: '7' } })
    fireEvent.click(await screen.findByText('Завести своё'))

    expect(await screen.findByText('Вернуть общее')).toBeTruthy()
  })

  it('отказ правки конвейера показан словами сервера', async () => {
    // Текст сервера говорит, чего не хватает. Своё «не удалось»
    // отправило бы человека нажимать ту же кнопку снова.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          return Promise.resolve(
            new Response(
              JSON.stringify({ error: 'У узла «написание» нет общего задания: копировать нечего' }),
              { status: 400 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.change(await screen.findByLabelText('Чей конвейер'), { target: { value: '7' } })
    fireEvent.click((await screen.findAllByText('Завести своё'))[0]!)
    expect(await screen.findByText(/копировать нечего/)).toBeTruthy()
  })

  it('модель узла видна в списке, не открывая задание', async () => {
    // Узлов больше пяти, и «какой узел какой моделью» — вопрос обо ВСЁМ
    // конвейере сразу. Открывая задания по одному, ответить на него
    // нельзя, а именно им и решают, где менять модель.
    serve(
      {
        prompts: [
          { ...ЗАДАНИЯ.prompts[0]!, model: 'дешёвая/быстрая' },
          { ...ЗАДАНИЯ.prompts[0]!, id: 'verify', name: 'Слепая сверка', model: '' },
        ],
      },
      КЛЮЧИ,
    )
    render(<Workshop me={МАСТЕР} />)
    // Ищем В СТРОКЕ списка заданий, а не по всей странице: конвейер
    // выше показывает те же имена моделей, и проверка по странице
    // зеленела бы на списке, который модель потерял.
    const строка = (await screen.findByText('Черновик задачи')).closest('.list-row')
    expect(строка?.textContent).toMatch(/дешёвая\/быстрая/)
    // У узла без своей модели сказано словами, а не пусто: пустое место
    // читается как «не прочиталось», и составитель пойдёт перезагружать
    // экран, который работает.
    const вторая = screen.getByText('Слепая сверка').closest('.list-row')
    expect(вторая?.textContent).toMatch(/модель поставщика/)
  })

  it('модель узла уезжает на сервер вместе с заданием', async () => {
    // Поле, которое набирается и не отправляется, — худший вид отказа:
    // сохранение отвечает успехом, редакция растёт, а конвейер ходит
    // прежней моделью. Замечают это по счёту через месяц.
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    const поле = await screen.findByPlaceholderText('моделью поставщика')
    fireEvent.change(поле, { target: { value: 'дорогая/рассуждающая' } })
    fireEvent.click(screen.getByText('Сохранить задание'))

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) =>
          String(call[0]).includes('/admin/api/prompts/draft') &&
          (call[1] as { method?: string })?.method === 'PUT',
      )
      expect(sent).toBeTruthy()
      const body = JSON.parse(String((sent![1] as { body?: string }).body))
      expect(body.model).toBe('дорогая/рассуждающая')
    })
  })

  it('снятая модель уезжает пустой, а не пропадает из запроса', async () => {
    // Пустая строка — это ВЫБОР «моделью поставщика». Не отправь студия
    // поля вовсе, и снять модель узла было бы нельзя: сервер оставил бы
    // прежнюю, а выглядело бы это как несохранение.
    serve({ prompts: [{ ...ЗАДАНИЯ.prompts[0]!, model: 'дорогая/рассуждающая' }] }, КЛЮЧИ)
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    const поле = await screen.findByDisplayValue('дорогая/рассуждающая')
    fireEvent.change(поле, { target: { value: '' } })
    fireEvent.click(screen.getByText('Сохранить задание'))

    await waitFor(() => {
      const sent = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.find(
        (call) =>
          String(call[0]).includes('/admin/api/prompts/draft') &&
          (call[1] as { method?: string })?.method === 'PUT',
      )
      expect(sent).toBeTruthy()
      const body = JSON.parse(String((sent![1] as { body?: string }).body))
      expect(body).toHaveProperty('model')
      expect(body.model).toBe('')
    })
  })

  it('модель, которой нет в прайсе, из поля не пропадает', async () => {
    // Список моделей — подсказка, а не словарь: новая модель появляется
    // у поставщика раньше, чем в нашем прайсе. Окажись поле списком
    // выбора, открытие задания молча сменило бы модель узла на первую
    // известную — и узнали бы об этом по счёту.
    serve({ prompts: [{ ...ЗАДАНИЯ.prompts[0]!, model: 'новейшая/которой-нет-в-прайсе' }] }, КЛЮЧИ)
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByDisplayValue('новейшая/которой-нет-в-прайсе')).toBeTruthy()
  })

  it('отказ списка моделей не ломает правку задания', async () => {
    // Список — подсказка. Без неё модель вписывается руками, и полоса
    // отказа рядом с работающим полем отправила бы составителя чинить
    // то, что не сломано.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string) => {
        if (String(path).startsWith('/admin/api/models')) {
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'Список моделей не прочитан' }), { status: 500 }),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Черновик задачи'))
    expect(await screen.findByPlaceholderText('моделью поставщика')).toBeTruthy()
    expect(screen.queryByText(/Список моделей не прочитан/)).toBeNull()
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
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
    // Право «задания» держит и задания моделям, и свод правил: оба
    // раздела говорят об этом своими словами.
    expect(screen.getAllByText(/кому выдано право «задания»/)).toHaveLength(2)
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
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
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
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

  it('копящееся правило показано числом, а не одним словом', async () => {
    // «Кандидат» без числа читается как «сломалось»: составитель идёт
    // чинить то, что работает. «2 из 3» читается как «копится», и делать
    // при этом не надо ничего.
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('копится: 2 из 3')).toBeTruthy()
    expect(screen.getByText('действует')).toBeTruthy()
  })

  it('видно, откуда правило взялось', async () => {
    // Правило, выведенное из замечаний, и правило, написанное человеком,
    // — разной силы при споре, и составитель обязан различать их
    // взглядом: гасить чужое накопленное и своё собственное это разные
    // решения.
    render(<Workshop me={МАСТЕР} />)
    expect(await screen.findByText('из замечаний')).toBeTruthy()
    expect(screen.getByText('встроенное')).toBeTruthy()
  })

  it('правило открывается на правку со своим текстом', async () => {
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    expect(
      await screen.findByDisplayValue('Не пиши в условии слово «ритуально» и однокоренные.'),
    ).toBeTruthy()
  })

  it('форма проверки строится по каталогу сервера, а не по своему списку', async () => {
    // Каталог предикатов закрыт и живёт в коде сервера. Повтори его
    // студия — он разошёлся бы молча ровно на том предикате, который
    // добавили последним, и составитель выбрал бы то, чего исполнитель
    // не знает.
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    const выбор = (await screen.findByLabelText('Проверяется машинно')) as HTMLSelectElement
    const пункты = Array.from(выбор.options).map((one) => one.value)
    // Первым — «без проверки»: правило без неё законно, а не недоделано.
    expect(пункты[0]).toBe('')
    expect(пункты.slice(1)).toEqual(КАТАЛОГ_ПРОВЕРОК.checks.map((one) => one.type))
    // Открылось на своей проверке, а не на пустом месте.
    expect(выбор.value).toBe('forbid-words')
    expect(await screen.findByDisplayValue('ритуально')).toBeTruthy()
  })

  it('у предиката показаны только его доводы', async () => {
    // Покажи форма все девять полей сразу — составитель заполнял бы
    // «сколько знаков» у запрета слов, а читает это поле никто: поле,
    // которое выглядит полем и никуда не идёт, хуже отсутствующего.
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    const выбор = await screen.findByLabelText('Проверяется машинно')
    expect(screen.queryByLabelText('Не больше')).toBeNull()

    fireEvent.change(выбор, { target: { value: 'length' } })
    expect(await screen.findByLabelText('Не больше')).toBeTruthy()
    expect(screen.queryByLabelText('Слова, которых быть не должно')).toBeNull()
    // И «только составитель» подписано там, где это правда.
    fireEvent.change(выбор, { target: { value: 'pattern' } })
    expect(await screen.findByText(/предикат модель не получает/)).toBeTruthy()
  })

  it('смена предиката не тащит доводы прежнего', async () => {
    // «Слова» у запрета и «выражение» у выражения — разные вещи.
    // Сохранись слова при переходе, они уехали бы на сервер незаметно
    // для того, кто их уже не видит, и вернулись бы, стоит выбрать
    // запрет снова.
    const calls: Array<Record<string, unknown>> = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          calls.push(JSON.parse(String(init.body)))
          return Promise.resolve(new Response(JSON.stringify(СВОД.rules[1]), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    fireEvent.change(await screen.findByLabelText('Проверяется машинно'), {
      target: { value: 'length' },
    })
    fireEvent.change(await screen.findByLabelText('Не больше'), { target: { value: '900' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    await waitFor(() => expect(calls.length).toBe(1))
    const check = calls[0]!.check as Record<string, unknown>
    expect(check.type).toBe('length')
    expect(check.max).toBe(900)
    expect(check.words).toBeUndefined()
  })

  it('слова принимаются и столбиком, а не только через запятую', async () => {
    // Составитель вставляет сюда столбик из чужого документа так же
    // часто, как набирает через запятую. Режь студия только по запятой —
    // весь столбик уехал бы ОДНИМ словом «алкоголь\nспиртное\nвино», и
    // проверка искала бы его целиком, то есть не находила бы никогда.
    // Правило при этом выглядело бы работающим.
    const calls: Array<Record<string, unknown>> = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          calls.push(JSON.parse(String(init.body)))
          return Promise.resolve(new Response(JSON.stringify(СВОД.rules[1]), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    fireEvent.change(await screen.findByLabelText('Слова, которых быть не должно'), {
      target: { value: 'алкоголь\nспиртное\n\nвино' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    await waitFor(() => expect(calls.length).toBe(1))
    const check = calls[0]!.check as Record<string, unknown>
    expect(check.words).toEqual(['алкоголь', 'спиртное', 'вино'])
  })

  it('«без проверки» шлёт снятие, а не молчание', async () => {
    // У поля на сервере три состояния, и «не слали» значит «не трогали».
    // Шли студия молчание вместо null, снять проверку было бы нельзя
    // вовсе: сохранение отвечало бы успехом, а проверка оставалась бы.
    const calls: Array<Record<string, unknown>> = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          calls.push(JSON.parse(String(init.body)))
          return Promise.resolve(new Response(JSON.stringify(СВОД.rules[1]), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    fireEvent.change(await screen.findByLabelText('Проверяется машинно'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    await waitFor(() => expect(calls.length).toBe(1))
    expect('check' in calls[0]!).toBe(true)
    expect(calls[0]!.check).toBeNull()
  })

  it('правка одного текста проверку не шлёт вовсе', async () => {
    // Нетронутую проверку сервер оставляет как была, и различает он это
    // по отсутствию поля. Шли студия прочитанное ею — правка одной
    // запятой переписывала бы проверку тем, что студия успела прочитать,
    // а прочитать она могла и устаревшее.
    const calls: Array<Record<string, unknown>> = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          calls.push(JSON.parse(String(init.body)))
          return Promise.resolve(new Response(JSON.stringify(СВОД.rules[1]), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    fireEvent.change(
      await screen.findByDisplayValue('Не пиши в условии слово «ритуально» и однокоренные.'),
      { target: { value: 'Не пиши «ритуально».' } },
    )
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    await waitFor(() => expect(calls.length).toBe(1))
    expect('check' in calls[0]!).toBe(false)
  })

  it('нынешняя проверка читается словами сервера', async () => {
    // Фразу пишет сервер. Собери её студия — составитель читал бы в
    // форме одно, а меряло бы другое, разойдясь на первой же правке
    // формулировки предиката.
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    expect(
      await screen.findByText(/в фрагментах условия не должно быть: ритуально/),
    ).toBeTruthy()
  })

  it('отказ по проверке показан словами сервера', async () => {
    // Сервер говорит, чего не хватает («не названо ни одного слова»).
    // Своё «не удалось сохранить» отправило бы составителя перебирать
    // поля вслепую.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                error: 'У проверки «Длина текста» не назван ни нижний предел, ни верхний',
              }),
              { status: 400 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    fireEvent.change(await screen.findByLabelText('Проверяется машинно'), {
      target: { value: 'length' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    expect(await screen.findByText(/не назван ни нижний предел/)).toBeTruthy()
  })

  it('погашение шлёт состояние, а не только текст', async () => {
    // Погасить правило — единственный способ остановить накопленное, не
    // стирая его. Уйди состояние мимо запроса, кнопка «сохранить»
    // работала бы на вид, а правило продолжало бы уходить в задание.
    const calls: Array<{ path: string; body: unknown }> = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (init?.method === 'PUT' && path.startsWith('/admin/api/rules')) {
          calls.push({ path, body: JSON.parse(String(init.body)) })
          return Promise.resolve(new Response(JSON.stringify(СВОД.rules[1]), { status: 200 }))
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByText('Слово «ритуально» в условии'))
    // Выбор показывает СЛОВО состояния, а не его код: составитель
    // читает список глазами, и «candidate» ему ничего не говорит.
    fireEvent.change(await screen.findByDisplayValue('копится'), {
      target: { value: 'muted' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить правило' }))

    await waitFor(() => expect(calls.length).toBe(1))
    expect(calls[0]!.path).toContain(encodeURIComponent('lint:term:7:риту'))
    expect((calls[0]!.body as { status?: string }).status).toBe('muted')
  })

  it('вытеснение из блока называется числом, а не молчанием', async () => {
    // То, ради чего уплотнение и заведено. Правило, не поместившееся в
    // блок, выглядит в списке действующим и до модели не доезжает;
    // узнать об этом составитель может только здесь. Показывай мы
    // «уплотнять нечего» и молчи про вытесненные — панель успокаивала бы
    // ровно в том случае, ради которого написана.
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (path.startsWith('/admin/api/rules/compaction')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                groups: [],
                before: 4200,
                after: 4200,
                limit: 3500,
                dropped: 6,
                note: '',
              }),
              { status: 200 },
            ),
          )
        }
        void init
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Предложить, что слить' }))
    expect(await screen.findByText(/не доезжает правил: 6/)).toBeTruthy()
    // И «сливать нечего» при этом тоже сказано: одно не заменяет другого.
    expect(screen.getByText(/Сливать нечего/)).toBeTruthy()
  })

  it('слить уходит только отмеченное составителем', async () => {
    // План приходит от модели, и составитель снимает с него галочки.
    // Уйди на сервер весь план целиком — снятая галочка не значила бы
    // ничего, а выглядела бы решением.
    const посланное: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((path: string, init?: RequestInit) => {
        if (path === '/admin/api/rules/compaction/apply') {
          посланное.push(JSON.parse(String(init?.body ?? '{}')))
          return Promise.resolve(
            new Response(
              JSON.stringify({
                asked: 1, merged: 1, failed: [],
                before: 900, after: 700, limit: 3500, dropped: 0,
                rules: СВОД.rules,
              }),
              { status: 200 },
            ),
          )
        }
        if (path.startsWith('/admin/api/rules/compaction')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                groups: [
                  { keepId: 'a', mergeIds: ['b'], text: 'Первая сводная.', why: 'оба про слог', saved: 120 },
                  { keepId: 'c', mergeIds: ['d'], text: 'Вторая сводная.', why: 'оба про разметку', saved: 80 },
                ],
                before: 900, after: 700, limit: 3500, dropped: 0, note: '',
              }),
              { status: 200 },
            ),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify(ответ(path, ЗАДАНИЯ, КЛЮЧИ, ПОЛЬЗОВАТЕЛИ, СВОД)), {
            status: 200,
          }),
        )
      }),
    )
    render(<Workshop me={МАСТЕР} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Предложить, что слить' }))
    await screen.findByText('Вторая сводная.')

    // Снимаем вторую группу. Ищем её галочку ПО ТЕКСТУ группы, а не по
    // месту в списке: на странице есть и другие галочки, и проверка,
    // считающая их по порядку, поехала бы от любой правки соседней
    // панели, ничего не сказав о своём предмете.
    fireEvent.click(screen.getByRole('checkbox', { name: /Вторая сводная/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Слить отмеченное' }))

    await waitFor(() => expect(посланное.length).toBe(1))
    const groups = (посланное[0] as { groups: { keepId: string }[] }).groups
    expect(groups.map((g) => g.keepId)).toEqual(['a'])
  })

  it('без права «задания» свод читается, но не правится', async () => {
    // Раздел не прячется: спрятанная вкладка при открытой ручке — это
    // подсказка, где искать, а не запрет. Прячется действие, и рядом
    // сказано, почему его нет.
    const смотрящий: Me = {
      login: 'смотрящий',
      displayName: 'Смотрящий',
      permissions: ['workshop'],
    }
    render(<Workshop me={смотрящий} />)
    expect(await screen.findByText('Слово «ритуально» в условии')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Написать правило' })).toBeNull()
    expect(screen.getByText(/Свод правит тот, кому выдано право/)).toBeTruthy()
    // Уплотнение — та же власть над сводом, и кнопка его прячется вместе
    // с остальными: показанная, она отказала бы правом на сервере, а
    // выглядела бы поломкой студии.
    expect(screen.queryByRole('button', { name: 'Предложить, что слить' })).toBeNull()
  })
})
