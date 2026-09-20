import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { ApiError } from './api'
import { Banner } from './components/Banner'

/// Чтение с сервера: три состояния вместо двух.
///
/// # Зачем крючок, если четырнадцать раз уже написано руками
///
/// Написано было одинаково и одинаково же сломано:
/// `try { setX(await api.y()) } catch { setFailure(…) }`, где `X` начинался
/// с `null`, а `null` означал «читаем». На отказе `X` так и оставался
/// `null`, и экран рисовал ОДНОВРЕМЕННО полосу отказа и «Читаем…» —
/// навсегда. Составитель без права `workshop` открывал Мастерскую, получал
/// внятный отказ по-русски и под ним вечное «Читаем…»: студия говорила
/// ему, что что-то ещё грузится, чего не будет никогда.
///
/// Чинить это в четырнадцати местах значит завести четырнадцатую копию
/// починки. Здесь состояние одно и разобрано союзом: «читаем», «отказ»,
/// «прочитано» — и третьего не дано. Показать отказ и «Читаем…» вместе
/// больше НЕЛЬЗЯ, и не потому, что так договорились: такого значения
/// просто нет.
export type Resource<T> = {
  /// Прочитать заново. Идёт к серверу и возвращается, когда ответ разобран.
  reload: () => Promise<void>

  /// Подменить прочитанное, не ходя к серверу.
  ///
  /// Нужно там, где действие уже вернуло новое значение: перечитывать его
  /// вторым запросом значит показать врачу старое между двумя ответами.
  set: (next: T) => void
} & (
  | { state: 'loading' }
  | { state: 'failed'; failure: string }
  | { state: 'ready'; value: T }
)

/// Заводит чтение и ведёт его состояние.
///
/// `read` обязан быть устойчивым между отрисовками — то есть
/// `useCallback`. Иначе чтение начинается заново на каждой отрисовке, и
/// экран уходит в вечный круг запросов.
///
/// `whenFailed` — что сказать, если сервер отказал без слов. Тексты
/// сервера всегда берутся его собственные: он пишет их по-русски и по
/// делу, а наш запасной говорит только о том, что именно не прочиталось.
export function useResource<T>(read: () => Promise<T>, whenFailed: string): Resource<T> {
  const [state, setState] = useState<
    { state: 'loading' } | { state: 'failed'; failure: string } | { state: 'ready'; value: T }
  >({ state: 'loading' })

  // Счётчик походов, а не флаг «идём»: два чтения в полёте — обычное дело
  // (сменили отбор, пока шло первое), и ответ ПЕРВОГО приходит вторым чаще,
  // чем кажется. Без счётчика он затирает свежий, и экран показывает отбор,
  // который составитель уже сменил, — молча и не воспроизводимо.
  const trip = useRef(0)

  const reload = useCallback(async () => {
    const mine = ++trip.current
    try {
      const value = await read()
      if (trip.current !== mine) return
      setState({ state: 'ready', value })
    } catch (error) {
      if (trip.current !== mine) return
      setState({
        state: 'failed',
        failure: error instanceof ApiError ? error.message : whenFailed,
      })
    }
  }, [read, whenFailed])

  useEffect(() => {
    void reload()
  }, [reload])

  const set = useCallback((next: T) => {
    // Тоже под счётчиком: подменённое значение обязано пережить ответ
    // чтения, которое шло рядом. Иначе действие составителя откатывается
    // само по себе через секунду после того, как он его сделал.
    trip.current++
    setState({ state: 'ready', value: next })
  }, [])

  return { ...state, reload, set }
}

/// Показывает прочитанное, а «читаем» и отказ берёт на себя.
///
/// Заведено не ради краткости: `Loaded` — единственное место, где вообще
/// решается, что показать вместо данных, и потому «отказ и „Читаем…“
/// разом» неоткуда взяться даже по невнимательности.
export function Loaded<T>({
  from,
  while: whileReading = 'Читаем…',
  children,
}: {
  from: Resource<T>
  /// Что писать, пока читаем. Своими словами: «Читаем цены…» говорит
  /// составителю, чего именно он ждёт, а общее «Читаем…» на экране с
  /// тремя разделами не говорит ничего.
  while?: string
  children: (value: T) => ReactNode
}) {
  if (from.state === 'loading') return <p className="empty">{whileReading}</p>
  if (from.state === 'failed') return <Banner kind="error">{from.failure}</Banner>
  return <>{children(from.value)}</>
}
