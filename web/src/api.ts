import type { Условие } from './traits'

// Обращение к редакционному API.
//
// Одно место на всю студию: токен, отказы и разбор ответа устроены
// одинаково везде. Разойдись они по экранам — и на одном экране отказ
// показывался бы человеку, а на другом падал бы в журнал браузера, где его
// никто не увидит.

/**
 * Признак того, что список показан не целиком.
 *
 * Отдельным типом на все списки с пределом: список, обрезанный молча, и
 * список, который весь помещается, выглядят одинаково, а решения по ним
 * принимаются разные. Оператор, увидевший пятьдесят однофамильцев без
 * пятьдесят первого, заводит второго врача тому, у кого запись есть;
 * составитель, увидевший пятьсот единиц из четырнадцати тысяч, заказывает
 * генерацию по тому, что считает целым классом.
 *
 * Поля необязательные: ручка, предела не имеющая, их не шлёт, и обходиться
 * без них показ обязан.
 */
export type Cut = {
  /** Сколько строк ручка отдаёт за раз. */
  limit?: number
  /** Подошло больше, чем показано. */
  more?: boolean
}

export type Source = {
  id: number
  slug: string
  kind: string
  title: string
  unitWord: string
  statementWord: string
  purpose: string
  hierarchy: string
  completeness: string
  edition: string
  status: string
}

export type Unit = {
  label: string
  parentLabel: string
  title: string
  path: string
  depth: number
  /** Род единицы: 'group' — вход в навигацию, 'entry' — то, по чему спрашивают. */
  kind: string
  answerable: boolean
  ord: number
}

export type Statement = {
  unitLabel: string
  kind: string
  designation: string
  body: string
  placeRef: string
  ord: number
}

export type Document = {
  id: number
  sourceId: number
  filename: string
  mime: string
  byteSize: number
  sha256: string
  uploadedBy: string
}

// Задание генерации, каким его видит студия.
//
// План сюда не едет: он весит килобайты, а на экране нужны состояние, шаг
// и то, что написано.
export type Job = {
  id: number
  /** Род работы: 'case' — пишется задача, 'parse' — разбирается документ. */
  kind: string
  sourceId: number
  unitLabel: string
  status: string
  step: string
  stepWord: string
  attempts: number
  error: string
  /**
   * Замечания о СДЕЛАННОЙ работе: какая часть документа не далась, сколько
   * непонятого отброшено из ответа модели. Отдельно от error, который
   * говорит, почему работа не сделана вовсе: свали их в одно, и задание с
   * одной потерянной частью из восьмидесяти читалось бы как провал.
   */
  notes: string[]
  createdAt: string
  updatedAt: string
  taskKind: string
  unitTitle: string
  unitWord: string
  /** Имя разбираемого файла. Только у разбора: номер документа человеку
   *  не говорит ничего. */
  documentName?: string
  drafts?: Draft[]
}

// Черновик задачи, как его написала модель. Поля названы ровно так, как
// их отдаёт сервер: переименование по дороге — это второе имя одного
// поля, и расходятся такие пары молча.
export type Draft = {
  // Опознаватель записанного черновика. Им и только им черновик
  // принимается задачей: без него кнопке «Принять» не на чем стоять.
  id: number
  title: string
  segments: { text: string; statements?: string[] }[]
  options: { label?: string; text: string }[]
  answer: string
  explanationMd: string
  difficulty: number
}

// Опубликованная задача: то, что доезжает до обучающегося. Поля названы
// ровно так, как их отдаёт сервер: переименование по дороге — это второе
// имя одного поля, и расходятся такие пары молча.
export type Case = {
  id: string
  sourceId: number
  unitLabel: string
  unitPath: string
  status: string
  statusWord: string
  revision: number
  origin: string
  body: CaseBody
  createdAt: string
  updatedAt: string
  publishedAt?: string
}

export type CaseBody = {
  title: string
  kind: string
  segments: { text: string; statements?: string[] }[]
  options: { label?: string; text: string }[]
  answer: string
  explanationMd: string
  difficulty: number
}

// Замечание, мешающее раздавать задачу. Сервер называет все разом, а не
// первое: правка идёт в один заход.
export type Fault = { where: string; what: string }

export type Prompt = {
  id: string
  name: string
  node: string
  nodeWord: string
  systemMd: string
  userMd: string
  revision: number
}

// Набор задач в списке: столько, сколько нужно, чтобы выбрать нужный.
export type Pack = {
  slug: string
  title: string
  status: string
  /** Линейка: чем набор открывается — guest, basic, paid, sponsored. */
  line: string
  cases: number
  /** Номер последнего выпуска; 0 — набор ещё не выпускался. */
  version: number
}

// Задача в составе набора. Название и метка единицы едут рядом с номером
// намеренно: состав из тридцати строк вида «c-1758…» нельзя ни проверить,
// ни пересобрать — по нему не видно даже, та ли это тема.
export type PackItem = {
  id: string
  ord: number
  title: string
  unitLabel: string
  status: string
}

// Набор целиком: карточка и состав. Число задач сюда не едет — вместо
// него сам состав, а считать его длину экран умеет и сам.
export type PackContents = Omit<Pack, 'cases'> & {
  summaryMd: string
  items: PackItem[]
  /**
   * Редакция набора — одна на карточку и состав.
   *
   * Уезжает обратно при сохранении: двое, открывшие «Кардиологию»,
   * иначе затирают друг друга молча. Первый двадцать минут переставляет
   * сорок задач, второй добавляет одну и сохраняет, первый сохраняет
   * следом — и добавленное исчезает.
   */
  revision: number
}

// Клиент глазами оператора. Одна и та же строка в списке и в карточке:
// оператор ищет человека и тут же решает, тот ли он, — и решает по тому
// же, что видел в списке.
export type Client = {
  id: number
  email: string
  displayName: string
  createdAt: string
  /** Пусто — врач ещё ни разу не заходил. */
  lastSeen: string
  blocked: boolean
  devices: number
  /** Живых прав: отозванное и истёкшее сюда не идут. */
  rights: number
}

// Группа врачей — гибкая роль пользователя приложения. Врач в группе,
// если подходит по правилу ИЛИ назван в ней поимённо.
export type Audience = {
  slug: string
  title: string
  note: string
  /** Признаки, соединённые «и». Пустое правило — НЕ «все»: только названные поимённо. */
  rule: Условие[]
  /** Сколько врачей названо поимённо. Это не размер группы: по правилу попадают и другие. */
  members: number
  /**
   * Почему правило не применяется. Пусто — применяется.
   *
   * Сломанное правило не раздаёт и не отнимает, и сказать об этом надо
   * словами: молча пустая группа читается как «никто не подошёл».
   */
  broken: string
}

// Врач, названный в группе поимённо.
export type AudienceMember = {
  account: number
  email: string
  addedBy: string
  addedAt: string
}

// Связь набора с группой: набор ей открыт или от неё скрыт.
export type PackAudience = {
  slug: string
  title: string
  mode: 'open' | 'hidden'
}

// Цена. Копейки, а не рубли: рубль с копейками, приехавший дробным
// числом, теряет копейку на первом же переводе в двоичную дробь.
export type Price = {
  purpose: string
  kopecks: number
  enabled: boolean
  /** Редакция строки цены; 0 означает, что цены на этот товар ещё нет. */
  revision?: number
}

export type Payment = {
  id: number
  source: string
  purpose: string
  kopecks: number
  status: string
  note: string
  by: string
  at: string
}

export type Entitlement = {
  kind: string
  pack: string
  origin: string
  startsAt: string
  /** Пусто — бессрочно. */
  expiresAt: string
}

// Сводка по задаче: как её разбирают.
export type CaseStats = {
  caseId: string
  attempts: number
  correct: number
  /** Доля верных разборов, 0…1. */
  solveRate: number
  medianMs: number
  /** Сколько раз выбран каждый неверный вариант. */
  confusion: Record<string, number>
  origin: string
  /**
   * Хватает ли попыток, чтобы доле верить. Без этого доля у задачи с
   * тремя попытками читается как приговор ей.
   */
  enough: boolean
}

export type EventCount = {
  name: string
  title: string
  count: number
  accounts: number
}

// Ключ программы: им сборка приложения представляется серверу. Секрета
// здесь нет и быть не может — он показывается один раз при заведении и в
// базе не хранится вовсе.
export type AppKey = {
  keyId: string
  title: string
  disabled: boolean
  createdAt: string
}

// Пользователь студии, каким его видит мастерская.
//
// Секрета аутентификатора здесь нет и быть не может: его показывают
// единственный раз — при заведении. Поле «секрет» в списке однажды
// кто-нибудь заполнил бы, и второй довод лёг бы рядом с первым.
export type StudioUser = {
  login: string
  displayName: string
  permissions: string[]
  disabled: boolean
  createdAt: string
}

export type Me = {
  login: string
  displayName: string
  permissions: string[]
}

// Отказ сервера, каким его показывают человеку.
//
// Текст берётся из ответа, а не сочиняется здесь: сервер пишет его
// по-русски и говорит, чего не хватает, — а «что-то пошло не так»
// отправляет человека перебирать поля вслепую.
export class ApiError extends Error {
  readonly status: number

  // Замечания, если сервер прислал их списком. Публикация задачи
  // отказывает именно так: составитель правит её в один заход, и отказ,
  // называющий одну беду из четырёх, заставляет ходить по кругу четырежды.
  // Потеряй мы список по дороге — и от него осталась бы одна первая
  // строка, то есть ровно то, от чего он заведён.
  readonly faults: Fault[]

  constructor(status: number, message: string, faults: Fault[] = []) {
    super(message)
    this.status = status
    this.faults = faults
  }
}

let token = ''

export function setToken(value: string): void {
  token = value
}

export function currentToken(): string {
  return token
}

/**
 * Кого звать, когда сессия кончилась.
 *
 * Без этого студия оставалась открытой на вид и мёртвой на деле: токен
 * протух, а колонка разделов, имя и права по-прежнему на месте.
 * Составитель правил задание генерации двадцать минут, жал «Сохранить»,
 * получал красную полосу — и жал ещё раз, потому что ничто не говорило
 * ему войти заново. Единственной дверью обратно было «Выйти», а оно
 * стирало набранное.
 *
 * Обработчик ставит App и вяжет к нему возврат на вход.
 */
let authLost: (() => void) | null = null

export function setAuthLost(handler: (() => void) | null): void {
  authLost = handler
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  /**
   * Не звать обработчика потерянной сессии на 401.
   *
   * Нужно ровно одному обращению — тому, которым студия при загрузке
   * спрашивает, открыта ли ещё сессия из печенья. Его 401 значит «не
   * входил», а не «вышел», и сказать в ответ «сессия кончилась» человеку,
   * который только что открыл студию впервые, было бы неправдой.
   */
  quiet401?: boolean,
): Promise<T> {
  const init: RequestInit = {
    method,
    headers: {},
    // Печенье сессии обязано уехать с обращением. Это и так умолчание
    // fetch для своего источника, но здесь на нём держится весь возврат к
    // открытой сессии, и умолчание, от которого зависит вход, называется
    // вслух.
    credentials: 'same-origin',
  }
  const headers = init.headers as Record<string, string>
  if (token) headers['Authorization'] = `Bearer ${token}`
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }

  const response = await fetch(path, init)
  const text = await response.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      // Ответ не разобрался — значит, до сервера не дошли вовсе (прокси,
      // страница входа, обрыв). Показывать человеку кусок HTML незачем.
      throw new ApiError(response.status, 'Сервер ответил непонятным: обновите страницу')
    }
  }
  if (!response.ok) {
    // Сессия кончилась или отозвана. Токен убирается сразу — иначе
    // следующее обращение уйдёт с тем же мёртвым, — и зовётся тот, кто
    // умеет спросить код заново.
    //
    // Вход исключён намеренно: неверный код тоже отвечает 401, и звать
    // на нём «сессия кончилась» значит объяснять человеку, что он вышел,
    // ровно в тот момент, когда он входит.
    if (response.status === 401 && path !== '/admin/api/login' && !quiet401) {
      token = ''
      authLost?.()
    }

    const failure = parsed as { error?: string; faults?: Fault[] } | null
    throw new ApiError(
      response.status,
      failure?.error ?? 'Сервер отказал без объяснения',
      failure?.faults ?? [],
    )
  }
  return parsed as T
}

export const api = {
  login: (login: string, code: string) =>
    request<{ token: string }>('POST', '/admin/api/login', { login, code }),

  me: () => request<Me>('GET', '/admin/api/me'),

  /**
   * Открыта ли ещё сессия — вопрос при загрузке страницы.
   *
   * Токен студии живёт в памяти страницы, а печенье сессии — в браузере, и
   * после перезагрузки в памяти нет ничего. Спросить некого, кроме сервера:
   * печенье HttpOnly студии не видно, и решить «я уже вошёл» она сама не
   * может. `null` значит «не входил» и никого ни о чём не извещает.
   */
  resume: () =>
    request<Me>('GET', '/admin/api/me', undefined, true).catch((error) => {
      if (error instanceof ApiError && error.status === 401) return null
      // Сервер не ответил вовсе — это не «не входил». Показать вход всё
      // равно придётся, но глотать отказ связи здесь нельзя: студия без
      // сервера не работает, и сказать об этом должен тот, кто спросил.
      throw error
    }),

  logout: () => request<{ status: string }>('POST', '/admin/api/logout'),

  sources: () => request<{ sources: Source[] }>('GET', '/admin/api/sources'),

  createSource: (source: Omit<Source, 'id' | 'status'>) =>
    request<{ id: number }>('POST', '/admin/api/sources', source),

  source: (id: number) => request<Source>('GET', `/admin/api/sources/${id}`),

  // Объявление источника действующим: до него источника для устройства не
  // существует — ни в списке справочников, ни в раздаче.
  setSourceStatus: (id: number, status: string) =>
    request<Source>('PUT', `/admin/api/sources/${id}/status`, { status }),

  // Пустой путь означает весь источник: срез по пути — то, ради чего путь
  // вообще считается, и отдельной ручки под «всё» заводить незачем.
  units: (id: number, path = '') =>
    request<Cut & { units: Unit[] }>(
      'GET',
      `/admin/api/sources/${id}/units${path ? `?path=${encodeURIComponent(path)}` : ''}`,
    ),

  statements: (id: number, unit = '') =>
    request<{ statements: Statement[] }>(
      'GET',
      `/admin/api/sources/${id}/statements${unit ? `?unit=${encodeURIComponent(unit)}` : ''}`,
    ),

  documents: (id: number) =>
    request<{ documents: Document[] }>('GET', `/admin/api/sources/${id}/documents`),

  upload: (id: number, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return request<{ id: number; format: string; fragments: number }>(
      'POST',
      `/admin/api/sources/${id}/documents`,
      form,
    )
  },

  fragments: (documentId: number) =>
    request<{ fragments: { ord: number; body: string; charFrom: number; charTo: number }[] }>(
      'GET',
      `/admin/api/documents/${documentId}/fragments`,
    ),

  draft: (documentId: number) =>
    request<{ units: Unit[]; statements: Statement[] }>(
      'GET',
      `/admin/api/documents/${documentId}/draft`,
    ),

  // Разбор документа моделью. Ответ — заведённое задание: ход виден в
  // списке заданий, а разобранное ложится черновиком и принимается
  // отдельным решением.
  parseDocument: (documentId: number) =>
    request<Job>('POST', `/admin/api/documents/${documentId}/parse`, {}),

  placeOrder: (
    sourceId: number,
    order: { unitLabel: string; kind?: string; targetStatement?: string; model?: string },
  ) => request<Job>('POST', `/admin/api/sources/${sourceId}/orders`, order),

  jobs: (sourceId: number, limit = 20) =>
    request<{ jobs: Job[] }>('GET', `/admin/api/sources/${sourceId}/jobs?limit=${limit}`),

  job: (id: number) => request<Job>('GET', `/admin/api/jobs/${id}`),

  cancelJob: (id: number) =>
    request<{ status: string }>('POST', `/admin/api/jobs/${id}/cancel`),

  prompts: () => request<{ prompts: Prompt[] }>('GET', '/admin/api/prompts'),

  savePrompt: (prompt: Pick<Prompt, 'id' | 'name' | 'systemMd' | 'userMd' | 'revision'>) =>
    request<{ id: string; revision: number }>('PUT', `/admin/api/prompts/${prompt.id}`, {
      name: prompt.name,
      systemMd: prompt.systemMd,
      userMd: prompt.userMd,
      revision: prompt.revision,
    }),

  cases: (query: { source?: number; path?: string; status?: string; limit?: number } = {}) => {
    const params = new URLSearchParams()
    if (query.source) params.set('source', String(query.source))
    if (query.path) params.set('path', query.path)
    if (query.status) params.set('status', query.status)
    if (query.limit) params.set('limit', String(query.limit))
    const tail = params.toString()
    return request<Cut & { cases: Case[] }>('GET', `/admin/api/cases${tail ? `?${tail}` : ''}`)
  },

  case: (id: string) => request<Case>('GET', `/admin/api/cases/${id}`),

  // Повтор отдаёт ПРЕЖНЮЮ задачу с пометкой repeated, а не вторую:
  // составитель нажал «Принять» дважды, а принял черновик один раз.
  caseFromDraft: (draftId: number) =>
    request<Case & { repeated: boolean }>('POST', '/admin/api/cases', { draftId }),

  saveCase: (id: string, body: CaseBody, revision: number) =>
    request<Case>('PUT', `/admin/api/cases/${id}`, { body, revision }),

  publishCase: (id: string) => request<Case>('POST', `/admin/api/cases/${id}/publish`),

  withdrawCase: (id: string) => request<Case>('POST', `/admin/api/cases/${id}/withdraw`),

  packs: () => request<{ packs: Pack[] }>('GET', '/admin/api/packs'),

  pack: async (slug: string) => {
    const raw = await request<{
      slug: string
      title: string
      summaryMd: string
      status: string
      version: number
      revision: number
      line: string
      cases: PackItem[]
    }>('GET', `/admin/api/packs/${encodeURIComponent(slug)}`)
    // Сервер зовёт состав «cases» — тем же словом, каким в списке наборов
    // зовётся их число. Разводим имена здесь, у самой двери: экран,
    // получающий под одним именем то число, то список, читается дважды.
    const { cases, ...card } = raw
    return { ...card, items: cases }
  },

  createPack: (pack: { slug: string; title: string; summaryMd: string; line: string }) =>
    request<{ slug: string }>('POST', '/admin/api/packs', pack),

  savePack: (
    slug: string,
    card: {
      title: string
      summaryMd: string
      status: string
      line: string
      revision: number
    },
  ) =>
    request<{ status: string; revision: number }>(
      'PUT',
      `/admin/api/packs/${encodeURIComponent(slug)}`,
      card,
    ),

  // Состав задаётся целиком: порядок списка и есть решение составителя, а
  // правка по одной задаче превращает его в череду мелких решений, из
  // которых порядок не виден никому.
  setPackItems: (slug: string, cases: string[], revision: number) =>
    request<{ cases: number; revision: number }>(
      'PUT',
      `/admin/api/packs/${encodeURIComponent(slug)}/items`,
      { cases, revision },
    ),

  releasePack: (slug: string) =>
    request<{ slug: string; version: number; cases: number; keyId: string }>(
      'POST',
      `/admin/api/packs/${encodeURIComponent(slug)}/releases`,
    ),

  clients: (q = '', limit = 50) =>
    request<Cut & { clients: Client[] }>(
      'GET',
      `/admin/api/clients?limit=${limit}${q ? `&q=${encodeURIComponent(q)}` : ''}`,
    ),

  client: (id: number) => request<Client>('GET', `/admin/api/clients/${id}`),

  setClientBlocked: (id: number, blocked: boolean) =>
    request<{ blocked: boolean }>('PUT', `/admin/api/clients/${id}/blocked`, { blocked }),

  clientPayments: (id: number) =>
    request<{ payments: Payment[] }>('GET', `/admin/api/clients/${id}/payments`),

  clientRights: (id: number) =>
    request<{ entitlements: Entitlement[] }>('GET', `/admin/api/clients/${id}/entitlements`),

  // Группы врачей. Словарь признаков приезжает вместе со списком: студия
  // рисует по нему выбор, и второй его редакции у неё быть не должно.
  audiences: () =>
    request<{ audiences: Audience[]; traits: string[]; windows: string[] }>(
      'GET',
      '/admin/api/audiences',
    ),

  audience: (slug: string) =>
    request<Audience>('GET', `/admin/api/audiences/${encodeURIComponent(slug)}`),

  createAudience: (group: { slug: string; title: string; note: string; rule: Условие[] }) =>
    request<{ slug: string }>('POST', '/admin/api/audiences', group),

  saveAudience: (slug: string, group: { title: string; note: string; rule: Условие[] }) =>
    request<{ slug: string }>('PUT', `/admin/api/audiences/${encodeURIComponent(slug)}`, group),

  dropAudience: (slug: string) =>
    request<{ dropped: string }>('DELETE', `/admin/api/audiences/${encodeURIComponent(slug)}`),

  // Размер спрашивается отдельно и тогда, когда на группу смотрят: счёт
  // проходит по всем учётным записям, а у группы с поведенческим
  // признаком — ещё и по всем попыткам. Считай его список, открытие
  // раздела дорожало бы ровно от того, что групп становится больше.
  audienceSize: (slug: string) =>
    request<{ size: number }>('GET', `/admin/api/audiences/${encodeURIComponent(slug)}/size`),

  audienceMembers: (slug: string) =>
    request<{ members: AudienceMember[] }>(
      'GET',
      `/admin/api/audiences/${encodeURIComponent(slug)}/members`,
    ),

  addAudienceMember: (slug: string, account: number) =>
    request<{ account: number }>(
      'POST',
      `/admin/api/audiences/${encodeURIComponent(slug)}/members`,
      { account },
    ),

  dropAudienceMember: (slug: string, account: number) =>
    request<{ account: number }>(
      'DELETE',
      `/admin/api/audiences/${encodeURIComponent(slug)}/members/${account}`,
    ),

  clientAudiences: (id: number) =>
    request<{ audiences: { slug: string; title: string }[] }>(
      'GET',
      `/admin/api/clients/${id}/audiences`,
    ),

  packAudiences: (slug: string) =>
    request<{ audiences: PackAudience[] }>(
      'GET',
      `/admin/api/packs/${encodeURIComponent(slug)}/audiences`,
    ),

  // Связи задаются целиком: «кому открыт» — одно решение, и правится оно
  // на одном экране. Правка по одной связи превратила бы его в череду
  // мелких шагов, из которых итог не виден тому, кто их делает.
  setPackAudiences: (slug: string, audiences: PackAudience[]) =>
    request<{ audiences: number }>(
      'PUT',
      `/admin/api/packs/${encodeURIComponent(slug)}/audiences`,
      { audiences: audiences.map((one) => ({ slug: one.slug, mode: one.mode })) },
    ),

  prices: () => request<{ prices: Price[] }>('GET', '/admin/api/prices'),

  setPrice: (price: Price) => request<Price>('PUT', '/admin/api/prices', price),

  // Ключ повтора обязателен и придумывается здесь: оператор, нажавший
  // дважды, не должен принять деньги дважды. Сервер отвечает на повтор
  // прежним платежом, а не отказом, — и это говорит оператору, что приход
  // уже оформлен.
  acceptPayment: (income: {
    accountId: number
    purpose: string
    kopecks: number
    idemKey: string
    note: string
  }) =>
    request<{
      id: number
      purpose: string
      kopecks: number
      status: string
      repeated: boolean
    }>('POST', '/admin/api/payments', income),

  refundPayment: (id: number, note: string) =>
    request<{ status: string }>('POST', `/admin/api/payments/${id}/refund`, { note }),

  reportCases: (limit = 20) =>
    request<{ floor: number; easy: CaseStats[]; hard: CaseStats[] }>(
      'GET',
      `/admin/api/reports/cases?limit=${limit}`,
    ),

  reportEvents: (days = 7) =>
    request<{ days: number; events: EventCount[] }>(
      'GET',
      `/admin/api/reports/events?days=${days}`,
    ),

  appKeys: () => request<{ keys: AppKey[] }>('GET', '/admin/api/app-keys'),

  // Ответ несёт сам ключ, а не его опознаватель: «key» — это секрет, и
  // отдаётся он единственный раз в жизни ключа. Второй раз его не покажет
  // никто — в базе лежит только отпечаток.
  issueAppKey: (key: { keyId: string; title: string }) =>
    request<{ key: string; note: string }>('POST', '/admin/api/app-keys', key),

  disableAppKey: (keyId: string) =>
    request<{ status: string }>(
      'POST',
      `/admin/api/app-keys/${encodeURIComponent(keyId)}/disable`,
    ),

  users: () => request<{ users: StudioUser[] }>('GET', '/admin/api/users'),

  // Ответ несёт секрет аутентификатора — единственный раз в жизни входа.
  // Дальше его не покажет никто: в базе он лежит, чтобы сверять коды, а
  // не чтобы его смотреть.
  createUser: (user: { login: string; displayName: string; permissions: string[] }) =>
    request<{ login: string; secret: string; note: string }>('POST', '/admin/api/users', user),

  setUserPermissions: (login: string, permissions: string[]) =>
    request<{ permissions: string[] }>(
      'PUT',
      `/admin/api/users/${encodeURIComponent(login)}/permissions`,
      { permissions },
    ),

  setUserDisabled: (login: string, disabled: boolean) =>
    request<{ disabled: boolean }>(
      'PUT',
      `/admin/api/users/${encodeURIComponent(login)}/disabled`,
      { disabled },
    ),

  contentVersion: () => request<{ version: number }>('GET', '/admin/api/content-version'),

  accept: (documentId: number) =>
    request<{ sourceId: number; accepted: number }>(
      'POST',
      `/admin/api/documents/${documentId}/accept`,
    ),
}
