import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { go } from './router'
import type { Check, Draft, Job, Me, Source, Unit } from './api'
import { Banner } from './components/Banner'

/**
 * Заказ задачи — отдельная страница.
 *
 * Заказ стоял разделом на экране источника, и довод был такой: заказывают
 * всегда ПО ЕДИНИЦЕ источника, а отдельный экран заставил бы переписывать
 * метку из одного списка в другой. Довод верный, и он никуда не делся —
 * поэтому источник и единица выбираются здесь списками, а не набираются
 * руками, а приходящий с экрана источника или из списка задач приносит
 * выбранный источник в адресе.
 *
 * Чего прежнее устройство не давало: заказать задачу, не открыв сперва
 * источник. «Напиши задачу по F41.1» — это ссылка, и теперь она есть.
 *
 * Страница показывает и очередь: заказ уходит в фон, и ждать его не надо
 * — составитель уходит работать дальше, а написанное находит здесь же.
 */
export function Generate({ me, source, unit }: { me: Me; source?: number; unit?: string }) {
  const canOrder = me.permissions.includes('generate')

  const readSources = useCallback(async () => (await api.sources()).sources, [])
  const sources = useResource(readSources, 'Список источников не прочитан')

  return (
    <div>
      <div className="page-head">
        <h2>Создать задачу</h2>
        <button onClick={() => go({ name: 'cases', query: {} })}>К задачам</button>
      </div>
      <p className="hint">
        Задачу пишет модель по положениям выбранной единицы источника, а
        потом другая модель решает её вслепую — не зная, какой ответ
        заказан. Разошлись — это видно здесь же, в задании.
      </p>

      {!canOrder && (
        <p className="hint">
          Заказать задачу может тот, кому выдано право на генерацию: заказ —
          это обращение к модели, и оно стоит денег.
        </p>
      )}

      <Loaded from={sources} while="Читаем источники…">
        {(list) =>
          list.length === 0 ? (
            // Пустая страница даёт выход с себя самой: заказывать не по
            // чему, пока нет источника, и отправлять человека искать
            // нужный раздел глазами незачем.
            <div className="empty stack">
              <p>
                Источников пока нет, а задача пишется по положениям источника.
                Заведите первый и принесите в него документ.
              </p>
              <div className="toolbar" style={{ justifyContent: 'center' }}>
                <button className="primary" onClick={() => go({ name: 'sources' })}>
                  К источникам
                </button>
              </div>
            </div>
          ) : (
            <Order me={me} sources={list} source={source ?? list[0]?.id ?? 0} unit={unit ?? ''} />
          )
        }
      </Loaded>
    </div>
  )
}

/**
 * Форма заказа и очередь по выбранному источнику.
 *
 * Выбранный источник стоит в адресе, а не в состоянии этой формы: заказ
 * по конкретному источнику — это ссылка, и «назад» должно возвращать к
 * прежнему источнику, а не выкидывать со страницы.
 */
function Order({
  me,
  sources,
  source,
  unit,
}: {
  me: Me
  sources: Source[]
  source: number
  unit: string
}) {
  const [kind, setKind] = useState('recognise')
  const [open, setOpen] = useState<Job | null>(null)
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)
  const [jobs, setJobs] = useState<Job[]>([])

  const canOrder = me.permissions.includes('generate')
  const выбранный = sources.find((one) => one.id === source)
  const словоЕдиницы = выбранный?.unitWord || 'единицу'

  // Единицы читаются по выбранному источнику. Пустой срез — весь
  // источник: выбирать из него и есть то, ради чего список показан.
  const readUnits = useCallback(async () => (await api.units(source)).units ?? [], [source])
  const units = useResource(readUnits, 'Единицы источника не прочитаны')

  const reload = useCallback(async () => {
    try {
      const loaded = await api.jobs(source)
      // Список без списка — пустой список, а не падение страницы.
      const list = loaded?.jobs ?? []
      setJobs(list)
      return list
    } catch (error) {
      // Очередь гасится, а не оставляется как была, и это не уборка.
      // Признак «идёт работа» считается по jobs; оставленный прежний
      // список держал его истинным, и опрос раз в три секунды не
      // прекращался никогда — в том числе на истёкшей сессии, где
      // каждое обращение отвечает отказом.
      setJobs([])
      setFailure(error instanceof ApiError ? error.message : 'Очередь не прочитана')
      return []
    }
  }, [source])

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
    if (!unit) return
    setBusy(true)
    setFailure('')
    try {
      await api.placeOrder(source, { unitLabel: unit, kind })
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

  // Повтор отказавшего задания.
  //
  // Кнопка нужна именно у отказавшего, и до неё выхода не было вовсе:
  // ключ повторности запирал единицу навсегда, и повторный заказ той же
  // единицы молча возвращал прежнее, закрытое задание. Выглядело это как
  // «нажал и ничего не произошло».
  async function retry(id: number) {
    setBusy(true)
    setFailure('')
    try {
      await api.retryJob(id)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Повтор не заказан')
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
    <>
      {failure && <Banner kind="error">{failure}</Banner>}

      <section className="page-section form-grid">
        <label className="form-row">
          <span className="fld-label">Источник</span>
          <select
            className="fld-long"
            value={String(source)}
            onChange={(e) =>
              // Смена источника сбрасывает единицу: метка чужого
              // источника в этом не значит ничего, а оставленная она
              // уехала бы в заказ и получила бы отказ сервера.
              go({ name: 'generate', source: Number(e.target.value) })
            }
          >
            {sources.map((one) => (
              <option key={one.id} value={one.id}>
                {one.title}
              </option>
            ))}
          </select>
        </label>

        <label className="form-row">
          <span className="fld-label">По чему писать</span>
          <Loaded from={units} while="Читаем единицы…">
            {(list) => {
              // Заказать можно только по той единице, по которой есть что
              // спрашивать: группа и единица без положений отказали бы на
              // сервере, и список, показывающий их, отправил бы человека
              // за отказом.
              const годные = list.filter((one: Unit) => one.answerable)
              if (годные.length === 0) {
                return (
                  <span className="empty">
                    В этом источнике ещё нет ни одной единицы с положениями.
                    Принесите в него документ и примите разбор.
                  </span>
                )
              }
              return (
                <select
                  className="fld-long"
                  value={unit}
                  onChange={(e) => go({ name: 'generate', source, unit: e.target.value })}
                >
                  <option value="">Выберите {словоЕдиницы}</option>
                  {годные.map((one: Unit) => (
                    <option key={one.label} value={one.label}>
                      {one.label} — {one.title}
                    </option>
                  ))}
                </select>
              )
            }}
          </Loaded>
        </label>

        <label className="form-row">
          <span className="fld-label">Что проверяет задача</span>
          <select className="fld-medium" value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="recognise">узнавание</option>
            <option value="action">действие</option>
          </select>
        </label>

        {canOrder && (
          <div className="form-actions">
            <button className="primary" onClick={() => void place()} disabled={busy || !unit}>
              Заказать задачу
            </button>
          </div>
        )}
        <p className="hint">
          Ждать не нужно: заказ уходит в очередь, а написанное появится
          ниже. Заведённая из черновика задача ложится в черновики — в
          раздачу её выпускают отдельным решением.
        </p>
      </section>

      <section className="page-section">
        <h3>Очередь</h3>
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
                {canOrder && (job.status === 'failed' || job.status === 'cancelled') && (
                  <button onClick={() => void retry(job.id)} disabled={busy}>
                    Повторить
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {open && <JobCard me={me} job={open} onClose={() => setOpen(null)} />}
    </>
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

/**
 * Написанное моделью по одному заданию — и решение составителя по нему.
 *
 * Принять черновик значит завести из него задачу: до этого момента
 * написанное моделью не существует ни для кого, кроме этого экрана. Именно
 * здесь конвейер и обрывался — деньги за обращение к модели платились,
 * черновик показывался, а выхода у него не было.
 *
 * Заведённая задача — черновик, а не раздача: выпускает её составитель на
 * странице задачи, отдельным решением и после сверки. Принять и выпустить
 * одной кнопкой значило бы отдать врачу то, чего никто не читал.
 */
function JobCard({ me, job, onClose }: { me: Me; job: Job; onClose: () => void }) {
  const drafts = job.drafts ?? []
  const canAccept = me.permissions.includes('case:write')
  /** Что вышло из принятия черновика: по опознавателю черновика. */
  const [accepted, setAccepted] = useState<Record<number, string>>({})
  const [busy, setBusy] = useState(0)
  const [failure, setFailure] = useState('')

  async function accept(draft: Draft) {
    setFailure('')
    setBusy(draft.id)
    try {
      const one = await api.caseFromDraft(draft.id)
      setAccepted((was) => ({ ...was, [draft.id]: one.id }))
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Черновик не принят')
    } finally {
      setBusy(0)
    }
  }

  return (
    <div className="page-section">
      <div className="page-head">
        <h3>
          {job.unitWord} {job.unitLabel} — {job.unitTitle}
        </h3>
        <button onClick={onClose}>Закрыть</button>
      </div>
      {job.error && <Banner kind="error">{job.error}</Banner>}
      {failure && <Banner kind="error">{failure}</Banner>}
      {!canAccept && drafts.length > 0 && (
        <p className="hint">Черновик принимает тот, кому выдано право «править задачи».</p>
      )}
      {drafts.length === 0 ? (
        <p className="empty">Написанного пока нет.</p>
      ) : (
        drafts.map((draft, i) => (
          <div key={i} className="fragment">
            <h4>{draft.title}</h4>
            {/* Черновик пишет модель, и объявленное обязательным она
                может не написать. Перебор отсутствующего бросает во время
                отрисовки — а отрисовка падает деревом целиком. */}
            <p>
              {(draft.segments ?? []).map((segment, j) => (
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
              {(draft.options ?? []).map((option, j) => (
                <li key={j}>
                  {option.label && <span className="mono">{option.label}</span>} {option.text}
                  {(option.label || option.text) === draft.answer && (
                    <span className="muted"> — заказанный ответ</span>
                  )}
                </li>
              ))}
            </ul>
            <p className="hint">{draft.explanationMd}</p>
            <CheckNote check={draft.check} />
            {accepted[draft.id] ? (
              // Выход к заведённой задаче даётся здесь же: принявший
              // черновик пришёл её выпускать, и искать её в списке
              // глазами незачем.
              <Banner kind="success">
                Задача заведена и лежит в черновиках.{' '}
                <button
                  className="link-button"
                  onClick={() => go({ name: 'case', id: accepted[draft.id] ?? '' })}
                >
                  Открыть её
                </button>
              </Banner>
            ) : (
              canAccept && (
                <div className="form-actions">
                  <button
                    className="primary"
                    disabled={busy !== 0}
                    onClick={() => void accept(draft)}
                  >
                    Принять черновик
                  </button>
                </div>
              )
            )}
          </div>
        ))
      )}
    </div>
  )
}

/**
 * Что сказала слепая сверка — рядом с черновиком, который она смотрела.
 *
 * До этого вердикт вычислялся и пропадал: за сверку платили, а составитель
 * её не видел, и задача, с которой сверка НЕ согласилась, выглядела ровно
 * как та, с которой согласилась.
 *
 * Три состояния, и все три разные. Сверки не было вовсе — задачу не
 * проверял никто. Сверка не состоялась — попытка была, и причина названа
 * словами: кончились деньги у поставщика, отменили задание, модель
 * вернула не тот JSON. Сверка прошла — сошлась или нет.
 *
 * Несогласие показывается тревогой, а непроверенность — предупреждением, и
 * путать их нельзя в обе стороны: тревога у непроверенной задачи приучает
 * не верить тревоге, а спокойный вид у несогласной отправляет задачу
 * врачу.
 */
function CheckNote({ check }: { check?: Check }) {
  if (!check) {
    return (
      <p className="hint">
        Слепой сверки у этого черновика нет: задачу не проверял никто.
        Прочитайте её сами, прежде чем принимать.
      </p>
    )
  }
  if (!check.done) {
    return (
      <Banner kind="error">
        Слепая сверка не состоялась: {check.note || 'причина не записана'}.
        Задача написана, но не проверена — повторите заказ или прочитайте её
        сами.
      </Banner>
    )
  }
  const verdict = check.verdict
  if (!verdict) {
    // Сверка объявила себя состоявшейся и не оставила ответа: так бывает у
    // черновика, записанного прежней выкаткой. Непонятое не применяется —
    // и уж точно не выдаётся за согласие.
    return <Banner kind="error">Слепая сверка отмечена прошедшей, но ответа её нет.</Banner>
  }
  if (verdict.agrees) {
    return (
      <p className="hint">
        Слепая сверка сошлась: по одному условию, не видя ни заказанной
        единицы, ни положений источника, она выбрала тот же ответ.
        {!verdict.sure && ' Уверенной она себя при этом не назвала.'}
      </p>
    )
  }
  return (
    <Banner kind="error">
      Слепая сверка НЕ сошлась: по одному условию она выбрала «{verdict.answer}
      ». {verdict.why} Это значит, что условие ведёт не к заказанному ответу —
      посмотрите сами, прежде чем принимать.
    </Banner>
  )
}
