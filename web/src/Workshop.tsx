import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { датой } from './words'
import { ApiError, api } from './api'
import type { AppKey, Me, Prompt } from './api'

// Мастерская: задания моделям и ключи программ.
//
// Оба раздела — настройка службы, а не работа над задачами, и закрыты они
// разными правами: задания правит тот, кто отвечает за качество
// генерации, ключи заводит тот, кто выкладывает сборки.

export function Workshop({ me }: { me: Me }) {
  return (
    <div>
      <Prompts me={me} />
      <AppKeys me={me} />
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
