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
 * Разделов семь, вложенности один уровень, отбор списка — три довода в
 * хвосте. Нужен ровно тот набор свойств, который даёт History API:
 * рабочие «назад» и «вперёд», ссылку можно отправить, обновление
 * страницы не выкидывает в начало.
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

/**
 * Отбор списка задач — целиком в адресе.
 *
 * Не в состоянии экрана, и это не мелочь: «покажи, что ты видишь»
 * решается ссылкой, а «назад» браузера возвращает к прежнему отбору, а не
 * выкидывает из списка целиком. Список задач — то место, где проводят
 * рабочий день, и отбор в нём составитель меняет десятки раз.
 */
export type CaseQuery = {
  /** Источник. Ноль или пусто — задачи по всем источникам. */
  source?: number
  /** Срез по пути единицы: «всё, что под 3». */
  path?: string
  /** Состояние: draft, review, published, archived. Пусто — все. */
  status?: string
  /** Поиск по названию, метке единицы и опознавателю. */
  q?: string
}

/** Куда открыта студия. */
export type Route =
  | { name: 'sources' }
  | { name: 'source'; id: number }
  | { name: 'cases'; query: CaseQuery }
  | { name: 'case'; id: string }
  | {
      name: 'generate'
      /** С каким источником открыть заказ. */
      source?: number
      /** С какой единицей источника. Пусто — составитель выберет сам. */
      unit?: string
    }
  | { name: 'packs' }
  | { name: 'sales' }
  | { name: 'audiences' }
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
    case 'cases':
    case 'case':
      return 'cases'
    case 'generate':
      return 'generate'
    case 'packs':
      return 'packs'
    case 'sales':
      return 'sales'
    case 'audiences':
      return 'audiences'
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
  const noHash = path.split('#')[0] ?? ''
  const bare = noHash.split('?')[0] ?? ''
  const search = new URLSearchParams(noHash.slice(bare.length))
  const parts = bare.split('/').filter((one) => one !== '')

  // Пустой путь — список задач: это первая строка колонки и то место,
  // где проводят рабочий день. Так же открывается студия донора. Прежде
  // здесь стоял список источников, и с переносом задач в свой раздел
  // получилось бы, что первая строка меню — одно, а открывается другое.
  if (parts.length === 0) return { name: 'cases', query: {} }

  if (parts[0] === 'cases') {
    if (parts.length === 1) {
      // Пустые доводы в отбор не кладутся: `{ source: 0, path: '' }` и
      // `{}` — один и тот же отбор, а сравнение адресов различило бы их,
      // и переход «на ту же страницу» перестал бы быть переходом на ту же.
      const query: CaseQuery = {}
      const source = Number(search.get('source'))
      if (Number.isInteger(source) && source > 0) query.source = source
      for (const key of ['path', 'status', 'q'] as const) {
        const value = search.get(key)
        if (value) query[key] = value
      }
      return { name: 'cases', query }
    }
    // Опознаватель задачи — строка, а не число: его выдаёт сервер, и
    // вида его студия не знает. Пустым он быть не может — пустой путь
    // сюда не доходит.
    if (parts.length === 2 && parts[1]) return { name: 'case', id: parts[1] }
    return { name: 'unknown', path: bare }
  }

  if (parts.length === 1 && parts[0] === 'generate') {
    const route: Route = { name: 'generate' }
    const source = Number(search.get('source'))
    if (Number.isInteger(source) && source > 0) route.source = source
    const unit = search.get('unit')
    if (unit) route.unit = unit
    return route
  }

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
    if (parts[0] === 'audiences') return { name: 'audiences' }
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
    case 'cases':
      return `/cases${tail({
        source: route.query.source ? String(route.query.source) : '',
        path: route.query.path ?? '',
        status: route.query.status ?? '',
        q: route.query.q ?? '',
      })}`
    case 'case':
      return `/cases/${encodeURIComponent(route.id)}`
    case 'generate':
      return `/generate${tail({
        source: route.source ? String(route.source) : '',
        unit: route.unit ?? '',
      })}`
    case 'unknown':
      return route.path
    default:
      return `/${route.name}`
  }
}

/**
 * Хвост адреса из непустых доводов.
 *
 * Пустые выбрасываются: `/cases?source=&path=&status=&q=` и `/cases` —
 * один и тот же отбор, но адреса разные, и сравнение «мы уже здесь»
 * различило бы их. Заодно ссылка, которую посылают друг другу, остаётся
 * читаемой.
 */
function tail(params: Record<string, string>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== '') search.set(key, value)
  }
  const out = search.toString()
  return out === '' ? '' : `?${out}`
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
