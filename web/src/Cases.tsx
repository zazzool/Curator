import { useCallback, useState } from 'react'

import { ApiError, api } from './api'
import { dropDraft, readDraft, writeDraft } from './draftStore'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import type { Case, CaseBody, Fault, Me, Source } from './api'
import { датойИвременем } from './words'
import { Banner } from './components/Banner'

// Задачи источника: что написано, что выверено, что раздаётся.
//
// Раздел стоит на экране источника по той же причине, что и генерация:
// задача принадлежит единице источника, и список задач в отрыве от
// источника пришлось бы читать по меткам, набранным руками.
export function Cases({ me, source, path }: { me: Me; source: Source; path: string }) {
  const [status, setStatus] = useState('')
  const [open, setOpen] = useState<Case | null>(null)
  const [failure, setFailure] = useState('')
  const [faults, setFaults] = useState<Fault[]>([])
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canWrite = me.permissions.includes('case:write')

  const read = useCallback(async () => {
    const loaded = await api.cases({ source: source.id, path, status })
    // Список без списка — пустой список, а не падение раздела: раздел,
    // не сумевший прочитать своё, обязан молчать в своих границах, а не
    // ронять белым весь экран источника.
    return loaded?.cases ?? []
  }, [source.id, path, status])
  const cases = useResource(read, 'Задачи не прочитаны')
  const reload = cases.reload

  // clear разводит два разных отказа: обычный и список замечаний. Смешай
  // их — и после неудачной публикации замечания висели бы поверх
  // следующего, уже другого отказа.
  function clear() {
    setFailure('')
    setFaults([])
    setNote('')
  }

  async function act(what: 'publish' | 'withdraw', id: string) {
    // Спрашивается только снятие: раздать задачу обратно можно той же
    // кнопкой, а снятую врач теряет из ленты и из повторения сразу —
    // и заметит это не здесь.
    if (
      what === 'withdraw' &&
      !confirmed(
        'Снять задачу с раздачи?',
        'Врачи перестанут получать её в ленте и в повторении. ' +
          'Из базы она не удаляется — попытки по ней остаются.',
      )
    ) {
      return
    }
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
          <select className="fld-medium" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">все</option>
            <option value="draft">черновики</option>
            <option value="review">на выверке</option>
            <option value="published">раздаются</option>
            <option value="archived">сняты с раздачи</option>
          </select>
        </label>
      </div>

      {failure && <Banner kind="error">{failure}</Banner>}
      {faults.length > 0 && (
        <ul className="units">
          {faults.map((fault, i) => (
            <li key={i}>
              <span className="mono">{fault.where}</span> {fault.what}
            </li>
          ))}
        </ul>
      )}
      {note && <Banner kind="success">{note}</Banner>}

      {!canWrite && (
        <p className="hint">
          Выпускать задачи в раздачу и снимать их может тот, кому выдано
          право править: выпуск — это решение о том, что теперь видят врачи.
        </p>
      )}

      {/* «Задач нет» — это ответ, и до ответа его писать нельзя: прежде
          список заводился пустым, и первую секунду каждого открытия
          составитель читал, что задач по источнику ещё нет. */}
      <Loaded from={cases} while="Читаем задачи…">
      {(list) => list.length === 0 ? (
        <p className="empty">
          {path
            ? 'Под этим путём задач нет. Проверьте метку — она пишется так же, как в документе.'
            : 'По этому источнику задач ещё нет. Закажите первую в разделе «Генерация».'}
        </p>
      ) : (
        <div className="list">
          {list.map((one) => (
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
                <button
                  className="danger"
                  onClick={() => act('withdraw', one.id)}
                  disabled={busy}
                >
                  Снять с раздачи
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      </Loaded>

      {open && (
        <CaseCard
          me={me}
          one={open}
          onClose={() => setOpen(null)}
          onSaved={(saved) => {
            setOpen(saved)
            void reload()
          }}
        />
      )}
    </section>
  )
}

/**
 * Задача целиком — и её правка.
 *
 * Правка живёт здесь, а не отдельным экраном: составитель правит то, что
 * сейчас читает, и переход на другой экран заставил бы его держать в
 * голове, что именно он там увидел. До этой правки право «править задачи»
 * обещало словами то, чего студия не умела вовсе: написанное моделью
 * можно было только прочитать.
 *
 * Раздаваемая задача не правится, и отказывает в этом сервер: правка
 * молча меняет то, что уже видят на устройствах, и расходится с
 * попытками, записанными по прежнему тексту. Снять с раздачи — отдельное
 * решение составителя. Здесь об этом сказано словами, чтобы он не искал
 * поломку там, где её нет.
 */
function CaseCard({
  me,
  one,
  onClose,
  onSaved,
}: {
  me: Me
  one: Case
  onClose: () => void
  onSaved: (saved: Case) => void
}) {
  const canWrite = me.permissions.includes('case:write')
  const раздаётся = one.status === 'published'
  const [editing, setEditing] = useState(false)

  if (editing) {
    return (
      <CaseEditor
        one={one}
        onClose={() => setEditing(false)}
        onSaved={(saved) => {
          setEditing(false)
          onSaved(saved)
        }}
      />
    )
  }

  return (
    <div className="page-section">
      <div className="page-head">
        <h2>{one.body.title || 'без названия'}</h2>
        {canWrite && !раздаётся && (
          <button onClick={() => setEditing(true)}>Править</button>
        )}
        <button onClick={onClose}>Закрыть</button>
      </div>
      {canWrite && раздаётся && (
        <p className="hint">
          Раздаваемая задача не правится: её уже видят на устройствах, и
          правка разошлась бы с попытками, записанными по прежнему тексту.
          Снимите её с раздачи, если нужно исправить.
        </p>
      )}
      <p className="hint">
        {one.statusWord} · {one.unitPath} · редакция {one.revision}
      </p>
      {/* Поля тела объявлены обязательными, но пишет их модель, и
          недописанное она отдаёт молча. Перебор отсутствующего бросает
          во время отрисовки, а отказ отрисовки снимает дерево целиком —
          граница отказа удержит его в этой панели, но и панель терять
          незачем, когда цена бережности два знака. */}
      <p>
        {(one.body?.segments ?? []).map((segment, i) => (
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
        {(one.body?.options ?? []).map((option, i) => (
          <li key={i}>
            {option.label && <span className="mono">{option.label}</span>} {option.text}
            {(option.label || option.text) === one.body?.answer && (
              <span className="muted"> — верный ответ</span>
            )}
          </li>
        ))}
      </ul>
      <p className="hint">{one.body.explanationMd}</p>
    </div>
  )
}

/**
 * Правка задачи.
 *
 * Правятся поля, а не текст целиком: условие разбито на фрагменты, и
 * разметка «какой фрагмент какое положение подтверждает» держится именно
 * на этом разбиении. Слей фрагменты в одно поле — и разметка исчезнет
 * молча, а она половина ценности задачи.
 *
 * Редакция сверяется на сервере, а не здесь: двое, открывшие задачу
 * одновременно, иначе затрут друг друга, и узнает об этом тот, чья правка
 * пропала, — по качеству задач через неделю.
 *
 * Набранное пишется в браузер на каждое изменение, тем же слоем, что и
 * задания моделям: правка задачи идёт теми же десятками минут, и F5 по
 * привычке стоит того же.
 */
function CaseEditor({
  one,
  onClose,
  onSaved,
}: {
  one: Case
  onClose: () => void
  onSaved: (saved: Case) => void
}) {
  const [body, setBody] = useState<CaseBody>(() => {
    const свой = readDraft<CaseBody>(one.id)
    // Черновик подхватывается только на своей редакции: набранный на
    // редакции, от которой сервер ушёл, дал бы дописать поверх чужой
    // правки, не показав её.
    if (свой !== null && свой.revision === one.revision) return свой.value
    if (свой !== null) dropDraft(one.id)
    return полноеТело(one.body)
  })
  const [restored] = useState(() => {
    const свой = readDraft<CaseBody>(one.id)
    return свой !== null && свой.revision === one.revision ? свой.savedAt : ''
  })
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)

  function правим(next: CaseBody) {
    setBody(next)
    writeDraft(one.id, next, one.revision)
  }

  async function save() {
    setFailure('')
    setBusy(true)
    try {
      const saved = await api.saveCase(one.id, body, one.revision)
      dropDraft(one.id)
      onSaved(saved)
    } catch (error) {
      // Текст сервера показывается как есть: отказ по редакции — не
      // поломка, а чужая правка, и своё «не удалось сохранить» отправило
      // бы человека нажимать ту же кнопку снова, поверх чужой работы.
      setFailure(error instanceof ApiError ? error.message : 'Задача не сохранена')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page-section">
      <div className="page-head">
        <h2>Правка задачи</h2>
        <button onClick={onClose}>Отменить</button>
      </div>
      <p className="hint">
        {one.statusWord} · {one.unitPath} · редакция {one.revision}
      </p>
      {failure && <Banner kind="error">{failure}</Banner>}
      {restored !== '' && (
        <Banner kind="info">
          Восстановлено несохранённое от {датойИвременем(restored)}.
        </Banner>
      )}

      <label className="form-row">
        <span className="fld-label">Название</span>
        <input
          className="fld-long"
          value={body.title}
          onChange={(e) => правим({ ...body, title: e.target.value })}
        />
      </label>

      <h3>Условие</h3>
      <p className="hint">
        Разметка держится на разбиении условия на фрагменты: у каждого
        фрагмента свои положения, и это то, чем задача отличается от
        вопроса с вариантами.
      </p>
      {body.segments.map((segment, i) => (
        <label key={i} className="form-row">
          <span className="fld-label">
            Фрагмент {i + 1}
            {segment.statements && segment.statements.length > 0 && (
              <span className="mono"> {segment.statements.join(', ')}</span>
            )}
          </span>
          <input
            className="fld-long"
            value={segment.text}
            onChange={(e) =>
              правим({
                ...body,
                segments: body.segments.map((было, j) =>
                  j === i ? { ...было, text: e.target.value } : было,
                ),
              })
            }
          />
        </label>
      ))}

      <h3>Варианты</h3>
      {body.options.map((option, i) => (
        <label key={i} className="form-row">
          <span className="fld-label">{option.label || `Вариант ${i + 1}`}</span>
          <input
            className="fld-long"
            value={option.text}
            onChange={(e) =>
              правим({
                ...body,
                options: body.options.map((было, j) =>
                  j === i ? { ...было, text: e.target.value } : было,
                ),
              })
            }
          />
        </label>
      ))}

      {/* Верный ответ выбирается из вариантов, а не пишется строкой:
          набранный руками, он расходится с вариантом на пробел или букву,
          и задача перестаёт иметь верный ответ — молча, потому что
          сверяется он текстом. */}
      <label className="form-row">
        <span className="fld-label">Верный ответ</span>
        <select
          className="fld-medium"
          value={body.answer}
          onChange={(e) => правим({ ...body, answer: e.target.value })}
        >
          <option value="">— не выбран —</option>
          {body.options.map((option, i) => (
            <option key={i} value={option.label || option.text}>
              {option.label ? `${option.label} — ${option.text}` : option.text}
            </option>
          ))}
        </select>
      </label>

      <label>
        <span className="fld-label">Разбор</span>
        <textarea
          value={body.explanationMd}
          onChange={(e) => правим({ ...body, explanationMd: e.target.value })}
        />
      </label>

      <div className="form-actions">
        <button className="primary" onClick={() => void save()} disabled={busy}>
          Сохранить задачу
        </button>
      </div>
    </div>
  )
}

/**
 * Тело со всеми списками на месте.
 *
 * Пишет их модель, и объявленное обязательным она может не написать.
 * Правка отсутствующего списка бросает на первом же нажатии клавиши — и
 * бросает во время отрисовки, то есть роняет панель целиком.
 */
function полноеТело(body: CaseBody): CaseBody {
  return {
    ...body,
    title: body?.title ?? '',
    segments: body?.segments ?? [],
    options: body?.options ?? [],
    answer: body?.answer ?? '',
    explanationMd: body?.explanationMd ?? '',
  }
}
