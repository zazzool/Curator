import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, api } from './api'
import type { Job, Me, Source, Unit } from './api'

// Генерация по источнику: заказать задачу, посмотреть очередь, прочитать
// черновик.
//
// Раздел стоит на экране источника, а не отдельной вкладкой, потому что
// заказывают всегда ПО ЕДИНИЦЕ источника: отдельный экран заставил бы
// человека переписывать метку из одного списка в другой, а метка, набранная
// руками, ошибается.
export function Generation({
  me,
  source,
  units,
}: {
  me: Me
  source: Source
  units: Unit[]
}) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [unitLabel, setUnitLabel] = useState('')
  const [kind, setKind] = useState('recognise')
  const [open, setOpen] = useState<Job | null>(null)
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)

  const canOrder = me.permissions.includes('generate')

  // Заказать можно только по той единице, по которой есть что спрашивать:
  // группа и единица без положений отказали бы на сервере, и список
  // вариантов, показывающий их, отправил бы человека получать отказ.
  const orderable = units.filter((unit) => unit.answerable)

  const reload = useCallback(async () => {
    try {
      const loaded = await api.jobs(source.id)
      // Список без списка — пустой список, а не падение раздела.
      // Обнаружилось проверкой: ответ без поля jobs ронял ВЕСЬ экран
      // источника белым — вместе с документами и принятым, к генерации
      // отношения не имеющими. Раздел, не сумевший прочитать своё, обязан
      // молчать в своих границах.
      const list = loaded?.jobs ?? []
      setJobs(list)
      return list
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Очередь не прочитана')
      return []
    }
  }, [source.id])

  const running = jobs.some((job) => job.status === 'queued' || job.status === 'running')

  // Опрос идёт, только пока в очереди есть незакрытое. Опрос «на всякий
  // случай» на открытой сутками вкладке даёт тысячи обращений в никуда, и
  // замечают это по счёту за трафик, а не по работе.
  const tick = useRef<number | undefined>(undefined)
  useEffect(() => {
    void reload()
  }, [reload])
  useEffect(() => {
    if (!running) return
    tick.current = window.setInterval(() => void reload(), 3000)
    return () => window.clearInterval(tick.current)
  }, [running, reload])

  async function place() {
    if (!unitLabel) return
    setBusy(true)
    setFailure('')
    try {
      await api.placeOrder(source.id, { unitLabel, kind })
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Заказ не поставлен')
    } finally {
      setBusy(false)
    }
  }

  async function cancel(id: number) {
    setBusy(true)
    setFailure('')
    try {
      await api.cancelJob(id)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Заказ не отменён')
    } finally {
      setBusy(false)
    }
  }

  async function show(id: number) {
    setFailure('')
    try {
      setOpen(await api.job(id))
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Задание не прочитано')
    }
  }

  return (
    <section className="page-section">
      <div className="page-head">
        <h2>Генерация</h2>
        {canOrder && (
          <span className="form-row">
            <select value={unitLabel} onChange={(e) => setUnitLabel(e.target.value)}>
              <option value="">Выберите {source.unitWord}</option>
              {orderable.map((unit) => (
                <option key={unit.label} value={unit.label}>
                  {unit.label} — {unit.title}
                </option>
              ))}
            </select>
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="recognise">узнавание</option>
              <option value="action">действие</option>
            </select>
            <button onClick={place} disabled={busy || !unitLabel}>
              Заказать задачу
            </button>
          </span>
        )}
      </div>

      {failure && <p className="banner error">{failure}</p>}

      {canOrder ? (
        <p className="hint">
          Задачу пишет модель по положениям выбранного {source.unitWord}а, а
          потом другая модель решает её вслепую — не зная, какой ответ
          заказан. Разошлись — это видно здесь же, в задании.
        </p>
      ) : (
        <p className="hint">
          Заказать задачу может тот, кому выдано право на генерацию:
          заказ — это обращение к модели, и оно стоит денег.
        </p>
      )}

      {orderable.length === 0 && canOrder && (
        <p className="empty">
          Заказывать пока не по чему. Список идёт за срезом по пути выше:
          если срез сужен, снимите его.
        </p>
      )}

      {jobs.length === 0 ? (
        <p className="empty">Заказов по этому источнику ещё не было.</p>
      ) : (
        <div className="list">
          {jobs.map((job) => (
            <div key={job.id} className="list-row">
              <span>
                <span className="mono">{job.unitLabel}</span> {job.unitTitle}
              </span>
              <span className="muted">{statusWord(job)}</span>
              <button onClick={() => show(job.id)}>Открыть</button>
              {canOrder && (job.status === 'queued' || job.status === 'running') && (
                <button onClick={() => cancel(job.id)} disabled={busy}>
                  Отменить
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {open && <JobCard job={open} onClose={() => setOpen(null)} />}
    </section>
  )
}

// statusWord — состояние задания словами составителя.
//
// «running» и «failed» ему ничего не говорят, а отказ показывается прямо в
// строке: причина, спрятанная за нажатием, не читается никем.
function statusWord(job: Job): string {
  switch (job.status) {
    case 'queued':
      return 'в очереди'
    case 'running':
      return `пишется: ${job.stepWord}`
    case 'done':
      return 'написана'
    case 'cancelled':
      return 'отменено'
    case 'failed':
      return job.error ? `не вышло: ${job.error}` : 'не вышло'
    default:
      return job.status
  }
}

function JobCard({ job, onClose }: { job: Job; onClose: () => void }) {
  const drafts = job.drafts ?? []
  return (
    <div className="page-section">
      <div className="page-head">
        <h2>
          {job.unitWord} {job.unitLabel} — {job.unitTitle}
        </h2>
        <button onClick={onClose}>Закрыть</button>
      </div>
      {job.error && <p className="banner error">{job.error}</p>}
      {drafts.length === 0 ? (
        <p className="empty">Написанного пока нет.</p>
      ) : (
        drafts.map((draft, i) => (
          <div key={i} className="fragment">
            <h3>{draft.title}</h3>
            <p>
              {draft.segments.map((segment, j) => (
                <span key={j}>
                  {segment.text}
                  {/* Разметка показывается прямо в условии: составитель
                      проверяет именно её — какой фрагмент какое положение
                      подтверждает, — и сноска под текстом заставила бы его
                      считать фрагменты глазами. */}
                  {segment.statements && segment.statements.length > 0 && (
                    <sup className="mono"> {segment.statements.join(', ')}</sup>
                  )}{' '}
                </span>
              ))}
            </p>
            <ul className="units">
              {draft.options.map((option, j) => (
                <li key={j}>
                  {option.label && <span className="mono">{option.label}</span>} {option.text}
                  {(option.label || option.text) === draft.answer && (
                    <span className="muted"> — заказанный ответ</span>
                  )}
                </li>
              ))}
            </ul>
            <p className="hint">{draft.explanationMd}</p>
          </div>
        ))
      )}
    </div>
  )
}
