import { useCallback, useState } from 'react'

import { ApiError, api } from './api'
import { dropDraft, readDraft, writeDraft } from './draftStore'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import { go } from './router'
import type { Case, CaseBody, Fault, Me } from './api'
import { датойИвременем } from './words'
import { Banner } from './components/Banner'
import { DifficultyDots } from './components/DifficultyDots'

/**
 * Задача целиком — её паспорт, текст и правка.
 *
 * Страница, а не панель внутри списка. Панелью она и была: задача
 * открывалась под списком, на экране источника, третьим разделом снизу, и
 * правка шла там же. Читалось это ровно до первой длинной задачи —
 * условие из десяти фрагментов уезжало вниз вместе со списком, а
 * вернуться к нему можно было только прокруткой.
 *
 * У страницы есть свой адрес, и это главное: «посмотри, что не так вот с
 * этой задачей» стало ссылкой. Устройство донорское — паспорт сверху,
 * условие с разметкой, ответ, разбор.
 *
 * Правка живёт здесь же, а не ещё одним экраном: составитель правит то,
 * что сейчас читает, и переход заставил бы его держать в голове, что
 * именно он там видел.
 */
export function CasePage({ me, id }: { me: Me; id: string }) {
  const read = useCallback(async () => await api.case(id), [id])
  const opened = useResource(read, 'Задача не прочитана')

  return (
    <div>
      <div className="page-head">
        <h2>Задача</h2>
        <button onClick={() => go({ name: 'cases', query: {} })}>К задачам</button>
      </div>
      <Loaded from={opened} while="Читаем задачу…">
        {(one) => <CaseCard me={me} one={one} onChanged={opened.set} />}
      </Loaded>
    </div>
  )
}

function CaseCard({
  me,
  one,
  onChanged,
}: {
  me: Me
  one: Case
  onChanged: (next: Case) => void
}) {
  const canWrite = me.permissions.includes('case:write')
  const раздаётся = one.status === 'published'
  const [editing, setEditing] = useState(false)
  const [failure, setFailure] = useState('')
  /** Замечания сервера: он называет все разом, а не первое — правка идёт в один заход. */
  const [faults, setFaults] = useState<Fault[]>([])
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  // Разводит два разных отказа: обычный и список замечаний. Смешай их — и
  // после неудачного выпуска замечания висели бы поверх следующего, уже
  // другого отказа.
  function clear() {
    setFailure('')
    setFaults([])
    setNote('')
  }

  async function act(what: 'publish' | 'withdraw') {
    // Спрашивается только снятие: раздать задачу обратно можно той же
    // кнопкой, а снятую врач теряет из ленты и из повторения сразу — и
    // заметит это не здесь.
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
      const done =
        what === 'publish' ? await api.publishCase(one.id) : await api.withdrawCase(one.id)
      onChanged(done)
      setNote(
        what === 'publish'
          ? 'Задача раздаётся. Устройства увидят её при следующей сверке версии.'
          : 'Задача снята с раздачи. Из базы она не удалена — попытки по ней остаются.',
      )
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

  if (editing) {
    return (
      <CaseEditor
        one={one}
        onClose={() => setEditing(false)}
        onSaved={(saved) => {
          setEditing(false)
          onChanged(saved)
        }}
      />
    )
  }

  return (
    <>
      <div className="page-head">
        <h3>{one.body?.title || 'без названия'}</h3>
        {canWrite && !раздаётся && <button onClick={() => setEditing(true)}>Править</button>}
        {canWrite && !раздаётся && (
          <button className="primary" onClick={() => void act('publish')} disabled={busy}>
            Раздавать
          </button>
        )}
        {canWrite && раздаётся && (
          <button className="danger" onClick={() => void act('withdraw')} disabled={busy}>
            Снять с раздачи
          </button>
        )}
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
      {canWrite && раздаётся && (
        <p className="hint">
          Раздаваемая задача не правится: её уже видят на устройствах, и
          правка разошлась бы с попытками, записанными по прежнему тексту.
          Снимите её с раздачи, если нужно исправить.
        </p>
      )}

      <Passport one={one} />

      <section className="page-section">
        <h3>Условие</h3>
        {/* Поля тела объявлены обязательными, но пишет их модель, и
            недописанное она отдаёт молча. Перебор отсутствующего бросает
            во время отрисовки, а отказ отрисовки снимает дерево целиком. */}
        <p>
          {(one.body?.segments ?? []).map((segment, i) => (
            <span key={i}>
              {segment.text}
              {/* Разметка показывается прямо в условии: составитель
                  проверяет именно её, а сноска под текстом заставила бы
                  его считать фрагменты глазами. */}
              {segment.statements && segment.statements.length > 0 && (
                <sup className="mono"> {segment.statements.join(', ')}</sup>
              )}{' '}
            </span>
          ))}
        </p>
      </section>

      <section className="page-section">
        <h3>Ответ</h3>
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
      </section>

      <section className="page-section">
        <h3>Разбор</h3>
        <p className="hint">{one.body?.explanationMd}</p>
      </section>
    </>
  )
}

/**
 * Паспорт задачи: то, чем она опознаётся и отбирается.
 *
 * Сведения о задаче, а не её содержание. Стоят выше условия по донорскому
 * порядку: составитель, открывший задачу по ссылке из чужого письма,
 * первым делом спрашивает «какая это и в каком она состоянии», а уже
 * потом читает текст.
 *
 * Опознаватель показывается целиком и моноширинным: его переписывают в
 * письма и в разговоры, и сокращённый он для этого не годится.
 */
function Passport({ one }: { one: Case }) {
  return (
    <section className="page-section">
      <h3>Паспорт</h3>
      <div className="table-wrap">
        <table className="table">
          <tbody>
            <tr>
              <th>Опознаватель</th>
              <td>
                <span className="mono">{one.id}</span>
              </td>
            </tr>
            <tr>
              <th>Состояние</th>
              <td>
                <span className={`tag ${one.status}`}>{one.statusWord}</span>
              </td>
            </tr>
            <tr>
              <th>Единица источника</th>
              <td>
                {/* Путь целиком, а не одна метка: по нему видно, где
                    единица стоит в источнике, и он же — то, чем задача
                    отбирается срезом. */}
                <span className="mono">{one.unitLabel}</span>{' '}
                <span className="muted">{one.unitPath}</span>
              </td>
            </tr>
            <tr>
              <th>Сложность</th>
              <td>
                <DifficultyDots level={one.body?.difficulty ?? 0} />
              </td>
            </tr>
            <tr>
              <th>Что проверяет</th>
              <td>{видомЗадачи(one.body?.kind ?? '')}</td>
            </tr>
            <tr>
              <th>Редакция</th>
              <td>{one.revision}</td>
            </tr>
            <tr>
              <th>Заведена</th>
              <td>{датойИвременем(one.createdAt)}</td>
            </tr>
            <tr>
              <th>Изменена</th>
              <td>{датойИвременем(one.updatedAt)}</td>
            </tr>
            {one.publishedAt && (
              <tr>
                <th>Выпущена</th>
                <td>{датойИвременем(one.publishedAt)}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/** Вид задачи словами составителя: «recognise» ему ничего не говорит. */
function видомЗадачи(kind: string): string {
  if (kind === 'recognise') return 'узнавание'
  if (kind === 'action') return 'действие'
  return kind || '—'
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
        <h3>Правка задачи</h3>
        <button onClick={onClose}>Отменить</button>
      </div>
      <p className="hint">
        {one.statusWord} · {one.unitPath} · редакция {one.revision}
      </p>
      {failure && <Banner kind="error">{failure}</Banner>}
      {restored !== '' && (
        <Banner kind="info">Восстановлено несохранённое от {датойИвременем(restored)}.</Banner>
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
