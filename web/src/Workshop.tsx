import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { датой } from './words'
import { ПРАВА, правоСловами } from './permissions'
import { ApiError, api } from './api'
import type { AppKey, Me, Prompt, StudioUser } from './api'

// Мастерская: пользователи студии, задания моделям и ключи программ.
//
// Всё это настройка службы, а не работа над задачами, и закрыто оно
// разными правами: задания правит тот, кто отвечает за качество
// генерации, ключи заводит тот, кто выкладывает сборки, а пользователей —
// тот, кому доверено раздавать доступ.

export function Workshop({ me }: { me: Me }) {
  return (
    <div>
      <Users me={me} />
      <Prompts me={me} />
      <AppKeys me={me} />
    </div>
  )
}

// Пользователи студии.
//
// До этого экрана нового составителя заводили на контуре руками. Ручка,
// которой нет, не безопаснее ручки под правом: она выносит ту же власть в
// ssh, где ни журнала, ни отказа за последнего мастера.
function Users({ me }: { me: Me }) {
  const [users, setUsers] = useState<StudioUser[] | null>(null)
  const [draft, setDraft] = useState<{ login: string; displayName: string; permissions: string[] }>(
    { login: '', displayName: '', permissions: [] },
  )
  const [made, setMade] = useState<{ login: string; secret: string; note: string } | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [failure, setFailure] = useState('')

  const canWorkshop = me.permissions.includes('workshop')

  const reload = useCallback(async () => {
    try {
      setUsers((await api.users())?.users ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Пользователи не прочитаны')
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  async function create(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    try {
      const out = await api.createUser(draft)
      setMade({ login: out.login, secret: out.secret, note: out.note })
      setDraft({ login: '', displayName: '', permissions: [] })
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Пользователь не заведён')
    }
  }

  async function togglePermission(user: StudioUser, code: string) {
    setFailure('')
    setMade(null)
    const next = user.permissions.includes(code)
      ? user.permissions.filter((one) => one !== code)
      : [...user.permissions, code]
    try {
      await api.setUserPermissions(user.login, next)
      await reload()
    } catch (error) {
      // Текст сервера показывается как есть: на последнем мастере он
      // говорит, что делать («сначала дайте право второму»), а своё «не
      // удалось» отправило бы человека нажимать ту же кнопку снова.
      setFailure(error instanceof ApiError ? error.message : 'Права не изменены')
    }
  }

  async function setDisabled(user: StudioUser, disabled: boolean) {
    setFailure('')
    setMade(null)
    try {
      await api.setUserDisabled(user.login, disabled)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Вход не изменён')
    }
  }

  return (
    <div>
      <div className="page-head">
        <h2>Пользователи студии</h2>
      </div>
      <p className="hint">
        Вход именованный: по журналу должно быть видно, кто что сделал.
        Отключённый вход не удаляется, а закрывается — имя в приходах и
        правках обязано остаться читаемым.
      </p>
      {!canWorkshop && (
        <p className="hint">Пользователей заводит тот, кому выдано право «мастерская».</p>
      )}

      {failure && <p className="alarm">{failure}</p>}

      {made && (
        <p className="done">
          Вход «{made.login}» заведён. {made.note}
          <br />
          <span className="label">{made.secret}</span>
        </p>
      )}

      <div className="page-section">
        {users === null ? (
          <p className="empty">Читаем…</p>
        ) : (
          <div className="list">
            {users.map((user) => (
              <div key={user.login} className="list-row">
                <div className="row-body">
                  <div className="row-line">
                    <span>
                      <span className="label">{user.login}</span> {user.displayName}
                      {user.disabled && <span className="kind">вход закрыт</span>}
                    </span>
                    <span className="row-tools">
                      <span className="muted">заведён {датой(user.createdAt)}</span>
                      {canWorkshop && (
                        <button onClick={() => setOpen(open === user.login ? null : user.login)}>
                          {open === user.login ? 'Свернуть' : 'Права'}
                        </button>
                      )}
                      {canWorkshop && (
                        <button onClick={() => setDisabled(user, !user.disabled)}>
                          {user.disabled ? 'Открыть вход' : 'Закрыть вход'}
                        </button>
                      )}
                    </span>
                  </div>
                  <div className="muted">
                    {user.permissions.length === 0
                      ? 'прав нет: войти сможет, а разделов не увидит'
                      : user.permissions.map(правоСловами).join(', ')}
                  </div>
                  {open === user.login && canWorkshop && (
                    <ul className="units">
                      {ПРАВА.map((right) => (
                        <li key={right.code}>
                          <label>
                            <input
                              type="checkbox"
                              checked={user.permissions.includes(right.code)}
                              onChange={() => togglePermission(user, right.code)}
                            />{' '}
                            {right.title} <span className="muted">— {right.about}</span>
                          </label>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {canWorkshop && (
        <form className="page-section form-grid" onSubmit={create}>
          <h3 className="sub">Завести вход</h3>
          <label className="form-row">
            <span className="fld-label">Имя входа</span>
            <input
              value={draft.login}
              onChange={(e) => setDraft({ ...draft, login: e.target.value })}
              placeholder="ivanova"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Как зовут</span>
            <input
              value={draft.displayName}
              onChange={(e) => setDraft({ ...draft, displayName: e.target.value })}
              placeholder="Иванова А. П."
            />
          </label>
          <ul className="units">
            {ПРАВА.map((right) => (
              <li key={right.code}>
                <label>
                  <input
                    type="checkbox"
                    checked={draft.permissions.includes(right.code)}
                    onChange={() =>
                      setDraft({
                        ...draft,
                        permissions: draft.permissions.includes(right.code)
                          ? draft.permissions.filter((one) => one !== right.code)
                          : [...draft.permissions, right.code],
                      })
                    }
                  />{' '}
                  {right.title} <span className="muted">— {right.about}</span>
                </label>
              </li>
            ))}
          </ul>
          <p className="hint">
            Секрет аутентификатора покажется один раз — сразу после
            заведения. Второй раз его не покажет никто: он хранится, чтобы
            сверять коды, а не чтобы его смотреть.
          </p>
          <div className="form-actions">
            <button className="primary" type="submit" disabled={draft.login.trim() === ''}>
              Завести вход
            </button>
          </div>
        </form>
      )}
    </div>
  )
}

// Задания моделям.
function Prompts({ me }: { me: Me }) {
  const [prompts, setPrompts] = useState<Prompt[] | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [draft, setDraft] = useState({ name: '', systemMd: '', userMd: '', revision: 0 })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canEdit = me.permissions.includes('prompts')

  const reload = useCallback(async () => {
    try {
      setPrompts((await api.prompts())?.prompts ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Задания моделей не прочитаны')
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  function edit(prompt: Prompt) {
    setOpen(prompt.id)
    setNote('')
    setFailure('')
    setDraft({
      name: prompt.name,
      systemMd: prompt.systemMd,
      userMd: prompt.userMd,
      revision: prompt.revision,
    })
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    if (open === null) return
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      const out = await api.savePrompt({ id: open, ...draft })
      await reload()
      setDraft({ ...draft, revision: out.revision })
      setNote(`Задание сохранено, редакция ${out.revision}.`)
    } catch (error) {
      // Отказ по редакции — не поломка, а чужая правка. Текст сервера
      // говорит об этом словами, и подменять его нечем: своё «не удалось
      // сохранить» отправило бы человека нажимать ту же кнопку снова.
      setFailure(error instanceof ApiError ? error.message : 'Задание не сохранено')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="page-head">
        <h2>Задания моделям</h2>
      </div>
      <p className="hint">
        Тем, что здесь написано, модель и пишет задачи. Правка меняет всё,
        что будет сгенерировано дальше, но не трогает написанного раньше.
      </p>
      {!canEdit && <p className="hint">Задания правит тот, кому выдано право «задания».</p>}

      {failure && <p className="alarm">{failure}</p>}
      {note && <p className="done">{note}</p>}

      <div className="page-section">
        {prompts === null ? (
          <p className="empty">Читаем…</p>
        ) : prompts.length === 0 ? (
          <p className="empty">Заданий нет: генерация не запустится.</p>
        ) : (
          <div className="list">
            {prompts.map((prompt) => (
              <button
                key={prompt.id}
                className="list-row"
                onClick={() => (open === prompt.id ? setOpen(null) : edit(prompt))}
              >
                <span>
                  {prompt.name}
                  <span className="kind">{prompt.nodeWord || prompt.node}</span>
                </span>
                <span className="muted">редакция {prompt.revision}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {open !== null && canEdit && (
        <form className="page-section form-grid" onSubmit={save}>
          <label className="form-row">
            <span className="fld-label">Как зовётся</span>
            <input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Что модель знает о себе</span>
            <textarea
              value={draft.systemMd}
              onChange={(e) => setDraft({ ...draft, systemMd: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Что ей поручается</span>
            <textarea
              value={draft.userMd}
              onChange={(e) => setDraft({ ...draft, userMd: e.target.value })}
            />
          </label>
          <p className="hint">
            Редакция сверяется при сохранении: двое, открывшие одно задание,
            иначе затрут друг друга молча — и узнает об этом тот, чья правка
            пропала, по качеству задач через неделю.
          </p>
          <div className="form-actions">
            <button className="primary" type="submit" disabled={busy}>
              Сохранить задание
            </button>
            <button type="button" onClick={() => setOpen(null)}>
              Закрыть
            </button>
          </div>
        </form>
      )}
    </div>
  )
}

// Ключи программ.
function AppKeys({ me }: { me: Me }) {
  const [keys, setKeys] = useState<AppKey[] | null>(null)
  const [draft, setDraft] = useState({ keyId: '', title: '' })
  const [issued, setIssued] = useState<{ keyId: string; key: string; note: string } | null>(null)
  const [failure, setFailure] = useState('')

  const canWorkshop = me.permissions.includes('workshop')

  const reload = useCallback(async () => {
    try {
      setKeys((await api.appKeys())?.keys ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Ключи программ не прочитаны')
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  async function issue(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    try {
      const out = await api.issueAppKey(draft)
      setIssued({ keyId: draft.keyId, key: out.key, note: out.note })
      setDraft({ keyId: '', title: '' })
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Ключ не заведён')
    }
  }

  async function disable(keyId: string) {
    setFailure('')
    setIssued(null)
    try {
      await api.disableAppKey(keyId)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Ключ не закрыт')
    }
  }

  return (
    <div>
      <div className="page-head">
        <h2>Ключи программ</h2>
      </div>
      <p className="hint">
        Ключом сборка приложения представляется серверу. Опознаватель ключа
        пишется латиницей: он едет в заголовке запроса, а заголовок
        кириллицы не принимает вовсе.
      </p>
      {!canWorkshop && (
        <p className="hint">Ключи заводит тот, кому выдано право «мастерская».</p>
      )}

      {failure && <p className="alarm">{failure}</p>}

      {issued && (
        <p className="done">
          Ключ {issued.keyId} заведён. {issued.note}
          <br />
          <span className="label">{issued.key}</span>
        </p>
      )}

      <div className="page-section">
        {keys === null ? (
          <p className="empty">Читаем…</p>
        ) : keys.length === 0 ? (
          <p className="empty">Ключей нет: ни одна сборка приложения к серверу не подключится.</p>
        ) : (
          <div className="list">
            {keys.map((key) => (
              <div key={key.keyId} className="list-row">
                <span>
                  <span className="label">{key.keyId}</span> {key.title}
                  {key.disabled && <span className="kind">закрыт</span>}
                </span>
                <span className="row-tools">
                  <span className="muted">заведён {датой(key.createdAt)}</span>
                  {canWorkshop && !key.disabled && (
                    <button onClick={() => disable(key.keyId)}>Закрыть</button>
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      {canWorkshop && (
        <form className="page-section form-grid" onSubmit={issue}>
          <label className="form-row">
            <span className="fld-label">Опознаватель</span>
            <input
              value={draft.keyId}
              onChange={(e) => setDraft({ ...draft, keyId: e.target.value })}
              placeholder="android-2026-09"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Для чего</span>
            <input
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
              placeholder="сборка для Android, сентябрь"
            />
          </label>
          <div className="form-actions">
            <button className="primary" type="submit">
              Завести ключ
            </button>
          </div>
        </form>
      )}
    </div>
  )
}
