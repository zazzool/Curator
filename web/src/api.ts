// Обращение к редакционному API.
//
// Одно место на всю студию: токен, отказы и разбор ответа устроены
// одинаково везде. Разойдись они по экранам — и на одном экране отказ
// показывался бы человеку, а на другом падал бы в журнал браузера, где его
// никто не увидит.

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
  sourceId: number
  unitLabel: string
  status: string
  step: string
  stepWord: string
  attempts: number
  error: string
  createdAt: string
  updatedAt: string
  taskKind: string
  unitTitle: string
  unitWord: string
  drafts?: Draft[]
}

// Черновик задачи, как его написала модель. Поля названы ровно так, как
// их отдаёт сервер: переименование по дороге — это второе имя одного
// поля, и расходятся такие пары молча.
export type Draft = {
  title: string
  segments: { text: string; statements?: string[] }[]
  options: { label?: string; text: string }[]
  answer: string
  explanationMd: string
  difficulty: number
}

export type Prompt = {
  id: string
  name: string
  node: string
  nodeWord: string
  systemMd: string
  userMd: string
  revision: number
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

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let token = ''

export function setToken(value: string): void {
  token = value
}

export function currentToken(): string {
  return token
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} }
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
    const message = (parsed as { error?: string } | null)?.error
    throw new ApiError(response.status, message ?? 'Сервер отказал без объяснения')
  }
  return parsed as T
}

export const api = {
  login: (login: string, code: string) =>
    request<{ token: string }>('POST', '/admin/api/login', { login, code }),

  me: () => request<Me>('GET', '/admin/api/me'),

  logout: () => request<{ status: string }>('POST', '/admin/api/logout'),

  sources: () => request<{ sources: Source[] }>('GET', '/admin/api/sources'),

  createSource: (source: Omit<Source, 'id' | 'status'>) =>
    request<{ id: number }>('POST', '/admin/api/sources', source),

  source: (id: number) => request<Source>('GET', `/admin/api/sources/${id}`),

  // Пустой путь означает весь источник: срез по пути — то, ради чего путь
  // вообще считается, и отдельной ручки под «всё» заводить незачем.
  units: (id: number, path = '') =>
    request<{ units: Unit[] }>(
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

  accept: (documentId: number) =>
    request<{ sourceId: number; accepted: number }>(
      'POST',
      `/admin/api/documents/${documentId}/accept`,
    ),
}
