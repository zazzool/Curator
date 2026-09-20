import { useCallback, useEffect, useState } from 'react'

import { api } from './api'
import { Loaded, useResource } from './useResource'
import { go } from './router'
import type { CaseQuery } from './router'
import type { Case, CaseCounts, Me, Source } from './api'
import { датой, счётом } from './words'
import { DifficultyDots } from './components/DifficultyDots'

/**
 * Рабочий список задач — отдельный раздел студии.
 *
 * Список стоял внутри экрана источника, третьим разделом снизу. Пока
 * источник был один, это читалось; с двумя источниками ответ на вопрос
 * «что у меня вообще написано» перестал существовать: чтобы увидеть все
 * задачи, надо было обойти все источники и сложить увиденное в голове.
 *
 * Устройство взято у донора, и у него же — довод порядка: список задач
 * это место, где проводят рабочий день, поэтому он стоит первым разделом,
 * а не последним разделом чужой страницы.
 *
 * **Вкладки состояний с числами.** Числа не украшение: по ним видно, что
 * на выверке набралось двенадцать, ещё до того как туда заглянуть. Считает
 * их сервер и считает без отбора по состоянию — иначе на открытой вкладке
 * стояло бы её число, а на остальных нули.
 *
 * **Таблица со столбцами, а не карточки.** Однородные записи сличают по
 * одному и тому же полю, и карточка заставляет искать это поле в разных
 * местах каждой строки. Правило записано в `web/CLAUDE.md` и куплено
 * разбором у донора.
 *
 * **Отбор целиком в адресе.** «Покажи, что ты видишь» решается ссылкой, а
 * «назад» браузера возвращает к прежнему отбору, а не выкидывает из
 * списка. Состояния экрана здесь нет вовсе, кроме набираемого в поиске.
 */
export function CaseList({ me, query }: { me: Me; query: CaseQuery }) {
  /**
   * Набранное в поиске — единственное, что живёт не в адресе.
   *
   * Адрес меняется с задержкой (ниже), и пока она идёт, поле обязано
   * показывать набранное. Подставь сюда адрес — и каждая третья буква
   * пропадала бы, возвращаясь к тому, что успело доехать.
   */
  const [typed, setTyped] = useState(query.q ?? '')

  // Адрес догоняет набранное через треть секунды. Без задержки каждое
  // нажатие клавиши — это шаг в истории браузера и запрос к серверу:
  // «F31.2» давало бы шесть шагов назад и шесть запросов, из которых
  // пять заказаны за то, чего составитель уже не ищет. Треть секунды:
  // пауза между нажатиями у печатающего человека короче, а осознанная
  // остановка длиннее.
  useEffect(() => {
    if (typed === (query.q ?? '')) return
    const timer = setTimeout(() => go({ name: 'cases', query: { ...query, q: typed } }), 300)
    return () => clearTimeout(timer)
  }, [typed, query])

  // Набранное подхватывает чужой переход: «назад» браузера и ссылка из
  // письма меняют отбор мимо этого поля, и оставленное как было оно
  // показывало бы прежний запрос поверх нового списка.
  useEffect(() => {
    setTyped(query.q ?? '')
  }, [query.q])

  const read = useCallback(async () => {
    const loaded = await api.cases({
      source: query.source,
      path: query.path,
      status: query.status,
      q: query.q,
    })
    // Список без списка — пустой список, а не падение раздела.
    return {
      cases: loaded?.cases ?? [],
      counts: loaded?.counts,
      limit: loaded?.limit ?? 0,
      more: loaded?.more ?? false,
    }
  }, [query.source, query.path, query.status, query.q])
  const cases = useResource(read, 'Задачи не прочитаны')

  // Источники читаются отдельно от задач: список их не меняется, а отбор
  // по ним меняется часто, и перечитывать справочник на каждую смену
  // вкладки незачем.
  const readSources = useCallback(async () => (await api.sources()).sources, [])
  const sources = useResource(readSources, 'Список источников не прочитан')

  const canOrder = me.permissions.includes('generate')

  function отобрать(next: Partial<CaseQuery>) {
    go({ name: 'cases', query: { ...query, ...next } })
  }

  return (
    <div>
      <div className="page-head">
        <h2>Задачи</h2>
        {canOrder && (
          // Синим отмечено то, ради чего раздел открывают чаще всего
          // после чтения: заказать новую. Ведёт на страницу заказа с
          // выбранным здесь источником — переписывать его руками не надо.
          <button
            className="primary"
            onClick={() => go({ name: 'generate', source: query.source })}
          >
            Создать задачу
          </button>
        )}
      </div>
      <p className="hint">
        Всё написанное по всем источникам. Задача попадает врачу только
        после того, как её выпустили: до этого она черновик и живёт здесь.
      </p>

      <Loaded from={cases} while="Читаем задачи…">
        {({ cases: list, counts, limit, more }) => (
          <>
            <Tabs counts={counts} status={query.status ?? ''} onPick={(status) => отобрать({ status })} />

            <div className="page-section">
              <div className="toolbar">
                <label className="inline-field">
                  <span className="fld-label">Найти</span>
                  <input
                    className="fld-medium"
                    value={typed}
                    onChange={(e) => setTyped(e.target.value)}
                    placeholder="название, метка или опознаватель"
                  />
                </label>
                <label className="inline-field">
                  <span className="fld-label">Источник</span>
                  <select
                    className="fld-long"
                    value={query.source ? String(query.source) : ''}
                    onChange={(e) => отобрать({ source: Number(e.target.value) || undefined })}
                  >
                    <option value="">все источники</option>
                    {sources.state === 'ready' &&
                      sources.value.map((one: Source) => (
                        <option key={one.id} value={one.id}>
                          {one.title}
                        </option>
                      ))}
                  </select>
                </label>
                <label className="inline-field">
                  <span className="fld-label">Срез по пути</span>
                  <input
                    className="fld-short"
                    value={query.path ?? ''}
                    onChange={(e) => отобрать({ path: e.target.value || undefined })}
                    placeholder="3 или F3"
                  />
                </label>
              </div>

              {list.length === 0 ? (
                <p className="empty">{пустоСловами(query)}</p>
              ) : (
                <>
                  <div className="table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th>Единица</th>
                          <th>Название</th>
                          <th>Сложность</th>
                          <th>Состояние</th>
                          <th>Откуда</th>
                          <th>Изменена</th>
                        </tr>
                      </thead>
                      <tbody>
                        {list.map((one) => (
                          <tr key={one.id}>
                            <td>
                              <span className="mono">{one.unitLabel}</span>
                            </td>
                            <td>
                              {/* Название — и есть дверь в задачу: ссылка
                                  на отдельной кнопке «Открыть» заставляет
                                  целиться в узкое место там, где строка и
                                  так вся про одну задачу. */}
                              <button
                                className="link-button"
                                onClick={() => go({ name: 'case', id: one.id })}
                              >
                                {one.body?.title || 'без названия'}
                              </button>
                            </td>
                            <td>
                              <DifficultyDots level={one.body?.difficulty ?? 0} />
                            </td>
                            <td>
                              <span className={`tag ${one.status}`}>{one.statusWord}</span>
                            </td>
                            <td className="muted">{откудаСловами(one)}</td>
                            <td className="muted">{датой(one.updatedAt)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>

                  <p className="hint">
                    {more
                      ? `Показаны первые ${limit}. Подошло больше — сузьте отбор, чтобы увидеть остальные.`
                      : `Показано ${счётом(list.length, 'задача', 'задачи', 'задач')}.`}
                  </p>
                </>
              )}
            </div>
          </>
        )}
      </Loaded>
    </div>
  )
}

/**
 * Вкладки состояний с числами.
 *
 * Порядок — от того, что требует работы, к тому, что убрано, и «Всё»
 * стоит первым: с него список и открывается.
 *
 * Числа приходят с сервера. Считать их здесь было бы нечем: в списке
 * лежат первые пятьдесят задач, и «Черновики 3» означало бы «три из
 * показанных пятидесяти» — то есть число, которое меняется от прокрутки.
 */
function Tabs({
  counts,
  status,
  onPick,
}: {
  counts: CaseCounts | undefined
  status: string
  onPick: (status: string) => void
}) {
  const ВКЛАДКИ: [string, string, (c: CaseCounts) => number][] = [
    ['', 'Всё', (c) => c.all],
    ['draft', 'Черновики', (c) => c.draft],
    ['review', 'На выверке', (c) => c.review],
    ['published', 'Раздаются', (c) => c.published],
    ['archived', 'Сняты', (c) => c.archived],
  ]

  return (
    <div className="tabs">
      {ВКЛАДКИ.map(([value, label, count]) => (
        <button
          key={value || 'все'}
          className={value === status ? 'tab tab-here' : 'tab'}
          aria-current={value === status ? 'page' : undefined}
          onClick={() => onPick(value)}
        >
          {label}
          {/* Число молчит, пока сервер его не прислал: ноль до ответа
              читается как «пусто», и составитель уходит из раздела, не
              дождавшись списка. */}
          {counts && <span className="muted"> {count(counts)}</span>}
        </button>
      ))}
    </div>
  )
}

/**
 * Почему список пуст — своими словами для каждого случая.
 *
 * Одно «Задач нет» на все случаи отправляет человека искать поломку:
 * отбор он ставил сам и про него помнит, а вот что отбор ничего не нашёл
 * — это и есть ответ.
 */
function пустоСловами(query: CaseQuery): string {
  if (query.q) {
    return `По запросу «${query.q}» ничего не нашлось. Ищется по названию, метке единицы и опознавателю.`
  }
  if (query.path) {
    return 'Под этим путём задач нет. Проверьте метку — она пишется так же, как в документе.'
  }
  if (query.status) {
    return 'В этом состоянии задач нет. Посмотрите на вкладке «Всё».'
  }
  if (query.source) {
    return 'По этому источнику задач ещё нет. Закажите первую — «Создать задачу».'
  }
  return 'Задач ещё нет. Заведите источник, принесите в него документ и закажите первую задачу.'
}

/**
 * Чем написана задача.
 *
 * Сервер пишет происхождение строкой вида «generated:логин»: кто заказал
 * и каким способом. Двоеточие составителю ничего не говорит, поэтому
 * показывается человеческая часть.
 */
function откудаСловами(one: Case): string {
  const [способ, кто] = one.origin.split(':')
  if (способ === 'generated') return кто ? `написана моделью, ${кто}` : 'написана моделью'
  return one.origin || '—'
}
