import { useEffect, useState } from 'react'

import type { SectionId } from './sections'

/**
 * Адреса студии.
 *
 * # Зачем они вообще
 *
 * До этого файла студия держала «где я» состоянием экрана: раздел лежал в
 * `useState`, открытый источник — рядом с ним. Работало это ровно до
 * первого рабочего вопроса. «Посмотри вот этот источник» приходилось
 * объяснять словами: ссылки на источник не существовало. «Назад» браузера
 * выкидывало из студии целиком — на страницу, с которой в неё вошли. F5
 * возвращал на список источников, чем бы составитель ни был занят
 * полчаса до того.
 *
 * Адрес чинит все три сразу, и ничего, кроме них, не делает.
 *
 * # Почему без библиотеки
 *
 * Разделов семь, вложенности один уровень, параметр один. Нужен ровно тот
 * набор свойств, который даёт History API: рабочие «назад» и «вперёд»,
 * ссылку можно отправить, обновление страницы не выкидывает в начало.
 * Зависимость ради этого не заводится — у студии их три, и четвёртая
 * должна оправдываться тем, чего своими руками не написать (так объявлено
 * в корневом CLAUDE.md, и `lucide-react` — именно такой случай).
 *
 * # Почему настоящие пути, а не решётка
 *
 * Решётка не требует ничего от сервера, и соблазн велик. Но адрес с
 * решёткой не попадает на сервер вовсе: ни в журнал обращений, ни в
 * будущую сверку прав. Путь честнее, и цена ему — подстановка `index.html`
 * на несуществующий файл (сервер, `studioFiles`), то есть двадцать строк
 * в одном месте.
 */

/** Куда открыта студия. */
export type Route =
  | { name: 'sources' }
  | { name: 'source'; id: number }
  | { name: 'packs' }
  | { name: 'sales' }
  | { name: 'reports' }
  | { name: 'workshop' }
  /**
   * Адрес, которого у студии нет.
   *
   * Отдельным состоянием, а не подстановкой списка источников. Сервер
   * теперь отдаёт студию на любой путь (иначе прямая ссылка на источник
   * не открылась бы), и опечатка в адресе доходит сюда. Показать на неё
   * список источников значит соврать: человек решит, что попал куда
   * хотел, и будет искать пропавшее в разделе, которого не открывал.
   */
  | { name: 'unknown'; path: string }

/**
 * В каком разделе стоит этот адрес.
 *
 * Считается из адреса, а не хранится рядом с ним: две записи об одном и
 * том же расходятся молча, и расходятся ровно там, где добавили страницу,
 * забыв дописать раздел. У адреса без раздела (ненайденный) раздела нет —
 * и колонка не подсвечивает ничего, потому что подсветить «Источники» на
 * ненайденном адресе значит сказать, что мы в Источниках.
 */
export function sectionOf(route: Route): SectionId | null {
  switch (route.name) {
    case 'sources':
    case 'source':
      return 'sources'
    case 'packs':
      return 'packs'
    case 'sales':
      return 'sales'
    case 'reports':
      return 'reports'
    case 'workshop':
      return 'workshop'
    default:
      return null
  }
}

/** Адрес раздела — то, куда ведёт его строка в колонке. */
export function sectionPath(id: SectionId): string {
  return `/${id}`
}

/** Разбор адреса. Принимает путь целиком, вместе с началом и хвостом. */
export function readRoute(path: string): Route {
  // Хвост после «?» и «#» к выбору страницы отношения не имеет: отбор
  // списка живёт в нём, и разбирает его сама страница.
  const bare = (path.split('#')[0] ?? '').split('?')[0] ?? ''
  const parts = bare.split('/').filter((one) => one !== '')

  if (parts.length === 0) return { name: 'sources' }

  if (parts[0] === 'sources') {
    if (parts.length === 1) return { name: 'sources' }
    // Опознаватель источника — число, и нечисло здесь не «источник 0», а
    // ненайденный адрес: запрос источника с номером 0 отказал бы на
    // сервере, и объяснять это пришлось бы отказом сервера вместо
    // внятного «такой страницы нет».
    const id = Number(parts[1])
    if (parts.length === 2 && Number.isInteger(id) && id > 0) {
      return { name: 'source', id }
    }
    return { name: 'unknown', path: bare }
  }

  if (parts.length === 1) {
    if (parts[0] === 'packs') return { name: 'packs' }
    if (parts[0] === 'sales') return { name: 'sales' }
    if (parts[0] === 'reports') return { name: 'reports' }
    if (parts[0] === 'workshop') return { name: 'workshop' }
  }

  return { name: 'unknown', path: bare }
}

/** Адрес страницы — то, что встанет в строку браузера и в ссылку. */
export function routePath(route: Route): string {
  switch (route.name) {
    case 'sources':
      return '/sources'
    case 'source':
      return `/sources/${route.id}`
    case 'unknown':
      return route.path
    default:
      return `/${route.name}`
  }
}

/**
 * Имя события о переходе внутри студии.
 *
 * `history.pushState` не будит `popstate` — это записано в самом
 * стандарте, и на это наступают все, кто пишет маршрутизацию руками:
 * адрес меняется, экран остаётся прежним. Поэтому о своём переходе студия
 * сообщает себе сама, а о чужом («назад», «вперёд») узнаёт от `popstate`.
 */
const MOVED = 'curator:moved'

/** Перейти по адресу, добавив шаг в историю браузера. */
export function go(route: Route): void {
  const path = routePath(route)
  if (path === window.location.pathname + window.location.search) return
  window.history.pushState(null, '', path)
  window.dispatchEvent(new Event(MOVED))
}

/**
 * Куда открыта студия сейчас.
 *
 * Читается из адреса на каждое изменение — своё и браузерное. Держать
 * разобранный адрес состоянием рядом с настоящим незачем: они разойдутся
 * на первом же переходе, которого этот крючок не увидел.
 */
export function useRoute(): Route {
  // Вместе с хвостом после «?»: отбор списка живёт в нём, и страница,
  // читающая отбор из адреса, обязана перерисоваться при его смене.
  // Разбор хвост отбрасывает сам — выбор страницы от него не зависит.
  const [path, setPath] = useState(() => window.location.pathname + window.location.search)

  useEffect(() => {
    const look = () => setPath(window.location.pathname + window.location.search)
    window.addEventListener('popstate', look)
    window.addEventListener(MOVED, look)
    return () => {
      window.removeEventListener('popstate', look)
      window.removeEventListener(MOVED, look)
    }
  }, [])

  return readRoute(path)
}

/**
 * Адрес раздела, уже разобранный.
 *
 * Колонка называет раздел, а переход идёт по адресу. Разбор берётся тот
 * же, каким студия читает адрес браузера: вторая таблица «раздел →
 * страница» разошлась бы с первой молча — и разошлась бы ровно на том
 * разделе, который добавили последним.
 */
export function sectionRoute(id: SectionId): Route {
  return readRoute(sectionPath(id))
}
