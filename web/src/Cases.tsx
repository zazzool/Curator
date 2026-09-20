import { useCallback, useEffect, useState } from 'react'

import { ApiError, api } from './api'
import type { Case, Fault, Me, Source } from './api'

// Задачи источника: что написано, что выверено, что раздаётся.
//
// Раздел стоит на экране источника по той же причине, что и генерация:
// задача принадлежит единице источника, и список задач в отрыве от
// источника пришлось бы читать по меткам, набранным руками.
export function Cases({ me, source, path }: { me: Me; source: Source; path: string }) {
  const [cases, setCases] = useState<Case[]>([])
  const [status, setStatus] = useState('')
  const [open, setOpen] = useState<Case | null>(null)
  const [failure, setFailure] = useState('')
  const [faults, setFaults] = useState<Fault[]>([])
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canWrite = me.permissions.includes('case:write')

  const reload = useCallback(async () => {
    try {
      const loaded = await api.cases({ source: source.id, path, status })
      // Список без списка — пустой список, а не падение раздела: раздел,
      // не сумевший прочитать своё, обязан молчать в своих границах, а не
      // ронять белым весь экран источника.
      setCases(loaded?.cases ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Задачи не прочитаны')
    }
  }, [source.id, path, status])

  useEffect(() => {
    void reload()
  }, [reload])

  // clear разводит два разных отказа: обычный и список замечаний. Смешай
  // их — и после неудачной публикации замечания висели бы поверх
  // следующего, уже другого отказа.
  function clear() {
    setFailure('')
    setFaults([])
    setNote('')
  }

  async function act(what: 'publish' | 'withdraw', id: string) {
    setBusy(true)
    clear()
    try {
      const done = what === 'publish' ? await api.publishCase(id) : await api.withdrawCase(id)
      setNote(
        what === 'publish'
          ? 'Задача раздаётся. Устройства увидят её при следующей сверке версии.'
          : 'Задача снята с раздачи. Из базы она не удалена — попытки по ней остаются.',
      )
      if (open?.id === id) setOpen(done)
      await reload()
    } catch (error) {
      if (error instanceof ApiError) {
        setFailure(error.message)
        setFaults(error.faults)
      } else {
        setFailure('Не вышло')
      }
    } finally {
      setBusy(false)
    }
  }

  async function show(id: string) {
    clear()
    try {
      setOpen(await api.case(id))
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Задача не прочитана')
    }
  }

  return (
    <section className="page-section">
      <div className="page-head">
        <h2>Задачи</h2>
        <label className="form-row">
          <span className="fld-label">Состояние</span>
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">все</option>
            <option value="draft">черновики</option>
            <option value="review">на выверке</option>
            <option value="published">раздаются</option>
            <option value="archived">сняты с раздачи</option>
          </select>
        </label>
      </div>

      {failure && <p className="banner error">{failure}</p>}
      {faults.length > 0 && (
        <ul className="units">
          {faults.map((fault, i) => (
            <li key={i}>
              <span className="mono">{fault.where}</span> {fault.what}
            </li>
          ))}
        </ul>
      )}
      {note && <p className="banner success">{note}</p>}

      {!canWrite && (
        <p className="hint">
          Выпускать задачи в раздачу и снимать их может тот, кому выдано
          право править: выпуск — это решение о том, что теперь видят врачи.
        </p>
      )}

      {cases.length === 0 ? (
        <p className="empty">
          {path
            ? 'Под этим путём задач нет. Проверьте метку — она пишется так же, как в документе.'
            : 'По этому источнику задач ещё нет. Закажите первую в разделе «Генерация».'}
        </p>
      ) : (
        <div className="list">
          {cases.map((one) => (
            <div key={one.id} className="list-row">
              <span>
                <span className="mono">{one.unitLabel}</span> {one.body.title || 'без названия'}
              </span>
              <span className="muted">{one.statusWord}</span>
              <button onClick={() => show(one.id)}>Открыть</button>
              {canWrite && one.status !== 'published' && (
                <button onClick={() => act('publish', one.id)} disabled={busy}>
                  Раздавать
                </button>
              )}
              {canWrite && one.status === 'published' && (
                <button onClick={() => act('withdraw', one.id)} disabled={busy}>
                  Снять с раздачи
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {open && <CaseCard one={open} onClose={() => setOpen(null)} />}
    </section>
  )
}

function CaseCard({ one, onClose }: { one: Case; onClose: () => void }) {
  return (
    <div className="page-section">
      <div className="page-head">
        <h2>{one.body.title || 'без названия'}</h2>
        <button onClick={onClose}>Закрыть</button>
      </div>
      <p className="hint">
        {one.statusWord} · {one.unitPath} · редакция {one.revision}
      </p>
      <p>
        {one.body.segments.map((segment, i) => (
          <span key={i}>
            {segment.text}
            {/* Разметка показывается прямо в условии: составитель
                проверяет именно её, а сноска под текстом заставила бы его
                считать фрагменты глазами. */}
            {segment.statements && segment.statements.length > 0 && (
              <sup className="mono"> {segment.statements.join(', ')}</sup>
            )}{' '}
          </span>
        ))}
      </p>
      <ul className="units">
        {one.body.options.map((option, i) => (
          <li key={i}>
            {option.label && <span className="mono">{option.label}</span>} {option.text}
            {(option.label || option.text) === one.body.answer && (
              <span className="muted"> — верный ответ</span>
            )}
          </li>
        ))}
      </ul>
      <p className="hint">{one.body.explanationMd}</p>
    </div>
  )
}
