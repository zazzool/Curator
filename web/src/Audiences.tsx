import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { счётом, датой } from './words'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import { ОКНА, ПРИЗНАКИ, признак, правилоСловами } from './traits'
import type { Условие } from './traits'
import type { Audience, Me } from './api'
import { Banner } from './components/Banner'

// Группы врачей: кому что открыто и почему.
//
// Раздел заведён затем, чтобы механизм существовал не только на сервере.
// Группа, которую нельзя ни завести, ни посмотреть, ни развязать с
// набором, — это ручка для того, кто пишет запросы руками, а решения о
// том, кому раздавать задачи, принимает не он.
//
// Речь здесь только о пользователях приложения. Права сотрудников студии
// — другой механизм, и он в Мастерской.

export function Audiences({ me }: { me: Me }) {
  const [open, setOpen] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState({ slug: '', title: '', note: '' })
  const [rule, setRule] = useState<Условие[]>([])
  const [failure, setFailure] = useState('')

  const canClients = me.permissions.includes('clients')

  // Отказ чтения живёт в самом чтении, а не в общем `failure`: смешай их
  // — и отказ заведения группы гасился бы удачным перечитыванием списка.
  const read = useCallback(async () => (await api.audiences())?.audiences ?? [], [])
  const groups = useResource(read, 'Группы не прочитаны')
  const reload = groups.reload

  async function create(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    try {
      // Карточка открывается по метке, которую вернул СЕРВЕР: он метку
      // приводит к своему виду, и открытая по набранной не нашлась бы.
      const { slug } = await api.createAudience({ ...draft, rule })
      setAdding(false)
      setDraft({ slug: '', title: '', note: '' })
      setRule([])
      await reload()
      setOpen(slug)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Группа не заведена')
    }
  }

  if (open !== null) {
    return <AudienceCard me={me} slug={open} onBack={() => { setOpen(null); void reload() }} />
  }

  return (
    <div>
      <div className="page-head">
        <h2>Группы</h2>
        {canClients && !adding && (
          <button className="primary" onClick={() => setAdding(true)}>
            Завести группу
          </button>
        )}
      </div>
      <p className="hint">
        Группа — это то, чем набор открывается помимо своей линейки. Врач в
        группе, если подходит по правилу или назван в ней поимённо.
      </p>

      {adding && (
        <form className="form-grid page-section" onSubmit={create}>
          <label className="form-row">
            <span className="fld-label">Метка</span>
            <input
              className="fld-medium"
              value={draft.slug}
              onChange={(e) => setDraft({ ...draft, slug: e.target.value })}
              placeholder="kafedra-terapii"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Название</span>
            <input
              className="fld-long"
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
              placeholder="Кафедра терапии"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Зачем заведена</span>
            <textarea
              value={draft.note}
              onChange={(e) => setDraft({ ...draft, note: e.target.value })}
            />
          </label>
          <RuleEditor rule={rule} onChange={setRule} />
          <div className="form-actions">
            <button className="primary" type="submit">
              Завести
            </button>
            <button type="button" onClick={() => setAdding(false)}>
              Не заводить
            </button>
          </div>
        </form>
      )}

      {failure && <Banner kind="error">{failure}</Banner>}

      <div className="page-section">
        <Loaded from={groups} while="Читаем группы…">
          {(list) =>
            list.length === 0 ? (
              // Выход с пустой страницы — там же, где пустота объявлена:
              // пришедший на пустой раздел пришёл его наполнять.
              <div className="empty stack">
                <p>
                  Групп пока нет. Пока их нет, наборы раздаются одной только
                  линейкой: гостевой всем, базовый привязавшему почту, платный
                  купившему.
                </p>
                {canClients && !adding && (
                  <div className="toolbar" style={{ justifyContent: 'center' }}>
                    <button className="primary" onClick={() => setAdding(true)}>
                      Завести группу
                    </button>
                  </div>
                )}
              </div>
            ) : (
              <div className="list">
                {list.map((group) => (
                  <button
                    key={group.slug}
                    className="list-row"
                    onClick={() => setOpen(group.slug)}
                  >
                    <span>
                      {group.title}
                      {/* Поломка правила — меткой в списке, а не только в
                          карточке: сломанная группа ничего не открывает и
                          ничего не скрывает, и узнать об этом нужно раньше,
                          чем врач спросит, куда делся набор. */}
                      {group.broken !== '' && (
                        <span className="tag retired">правило не разобрано</span>
                      )}
                    </span>
                    <span className="muted">{правилоСловами(group.rule)}</span>
                  </button>
                ))}
              </div>
            )
          }
        </Loaded>
      </div>
    </div>
  )
}

// Правило: признаки, соединённые «и».
//
// «Или» здесь нет намеренно, и это не недоделка: набор открывается сразу
// нескольким группам, и это то же самое «или», только видно его на
// карточке набора. Дерево со скобками — язык запросов, вписанный в
// студию, и правильность такого правила потом не проверит никто.
function RuleEditor({
  rule,
  onChange,
}: {
  rule: Условие[]
  onChange: (next: Условие[]) => void
}) {
  function set(index: number, next: Условие) {
    onChange(rule.map((one, i) => (i === index ? next : one)))
  }

  return (
    <div className="form-row">
      <span className="fld-label">Правило</span>
      <div className="stack">
        {rule.length === 0 && (
          <p className="hint">
            Правила нет: в группу попадут только те, кого назовут поимённо.
            Это и нужно кафедре — общего признака у её ординаторов нет.
          </p>
        )}
        {rule.map((one, index) => {
          const known = признак(one.trait)
          return (
            <div key={index} className="row-line">
              <span className="row-tools">
                <select
                  aria-label="Признак"
                  value={one.trait}
                  onChange={(e) => set(index, { ...one, trait: e.target.value })}
                >
                  {ПРИЗНАКИ.map((item) => (
                    <option key={item.code} value={item.code}>
                      {item.title}
                    </option>
                  ))}
                </select>
                {known?.порог !== undefined && (
                  <label>
                    <span className="visually-hidden">Порог</span>
                    <input
                      className="fld-short"
                      type="number"
                      min={0}
                      value={one.n ?? 0}
                      onChange={(e) => set(index, { ...one, n: Number(e.target.value) })}
                    />{' '}
                    {known.порог}
                  </label>
                )}
                {known?.заОкно === true && (
                  <select
                    aria-label="За какой срок"
                    value={one.over ?? 'all'}
                    onChange={(e) => set(index, { ...one, over: e.target.value })}
                  >
                    {ОКНА.map((window) => (
                      <option key={window.code} value={window.code}>
                        {window.title}
                      </option>
                    ))}
                  </select>
                )}
                <label>
                  <input
                    type="checkbox"
                    checked={one.not === true}
                    onChange={(e) => set(index, { ...one, not: e.target.checked })}
                  />{' '}
                  наоборот
                </label>
                <button
                  type="button"
                  onClick={() => onChange(rule.filter((_, i) => i !== index))}
                >
                  Убрать
                </button>
              </span>
            </div>
          )
        })}
        <div className="form-actions">
          <button
            type="button"
            onClick={() => onChange([...rule, { trait: ПРИЗНАКИ[0]?.code ?? '', n: 0 }])}
          >
            Добавить признак
          </button>
        </div>
        {rule.length > 0 && <p className="hint">Читается так: {правилоСловами(rule)}</p>}
      </div>
    </div>
  )
}

// Карточка группы: правило, размер и поимённый состав.
function AudienceCard({ me, slug, onBack }: { me: Me; slug: string; onBack: () => void }) {
  const [card, setCard] = useState({ title: '', note: '' })
  const [rule, setRule] = useState<Условие[]>([])
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [adding, setAdding] = useState('')
  // Размер спрашивается по нажатию, а не при открытии: счёт проходит по
  // всем учётным записям, а у группы с поведенческим признаком — ещё и по
  // всем попыткам. Считай его список групп, раздел дорожал бы ровно от
  // того, что групп становится больше.
  const [size, setSize] = useState<number | null>(null)

  const canClients = me.permissions.includes('clients')

  const read = useCallback(async () => {
    const [one, members] = await Promise.all([
      api.audience(slug),
      api.audienceMembers(slug),
    ])
    return { one, members: members?.members ?? [] }
  }, [slug])
  const opened = useResource(read, 'Группа не прочитана')
  const reload = opened.reload
  const loaded = opened.state === 'ready' ? opened.value : null
  const group: Audience | null = loaded?.one ?? null

  // Набранное заводится с прочитанного и переписывается только НОВЫМ
  // ответом сервера, а не каждой отрисовкой: иначе правка правила
  // откатывалась бы на любое перечитывание.
  useEffect(() => {
    if (group === null) return
    setCard({ title: group.title, note: group.note })
    setRule(group.rule)
  }, [group])

  async function save() {
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      await api.saveAudience(slug, { ...card, rule })
      await reload()
      // Размер, посчитанный до правки, относится к прежнему правилу, и
      // оставить его на экране значило бы соврать числом.
      setSize(null)
      setNote('Группа сохранена. Состав пересчитается сам, ночного пересчёта у неё нет.')
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Группа не сохранена')
    } finally {
      setBusy(false)
    }
  }

  async function count() {
    setFailure('')
    setBusy(true)
    try {
      const out = await api.audienceSize(slug)
      setSize(out?.size ?? 0)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Размер не посчитан')
    } finally {
      setBusy(false)
    }
  }

  async function add(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    const account = Number(adding)
    if (!Number.isInteger(account) || account <= 0) {
      setFailure('Номер врача — это целое число. Его видно на карточке врача в «Продажах».')
      return
    }
    setBusy(true)
    try {
      await api.addAudienceMember(slug, account)
      setAdding('')
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Врач не добавлен')
    } finally {
      setBusy(false)
    }
  }

  async function drop(account: number) {
    if (
      !confirmed(
        `Убрать врача № ${account} из группы «${card.title}»?`,
        'Наборы, открытые ему этой группой, из его приложения пропадут. Купленное остаётся.',
      )
    ) {
      return
    }
    setFailure('')
    setBusy(true)
    try {
      await api.dropAudienceMember(slug, account)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Врач не убран')
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    if (
      !confirmed(
        `Убрать группу «${card.title}»?`,
        'Она перестанет существовать. Группу, которой открыт хоть один набор, убрать нельзя — ' +
          'сперва развяжите наборы на их карточках.',
      )
    ) {
      return
    }
    setFailure('')
    setBusy(true)
    try {
      await api.dropAudience(slug)
      onBack()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Группа не убрана')
    } finally {
      setBusy(false)
    }
  }

  if (loaded === null || group === null) {
    return (
      <div>
        <div className="page-head">
          <h2>Группа</h2>
          <button onClick={onBack}>К группам</button>
        </div>
        <Loaded from={opened} while="Читаем группу…">
          {() => null}
        </Loaded>
      </div>
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>{group.title}</h2>
        <button onClick={onBack}>К группам</button>
      </div>
      <p className="hint">{group.slug}</p>

      {group.broken !== '' && (
        <Banner kind="error">
          Правило не разобрано, и группа поэтому не применяется ни в ту, ни в
          другую сторону: она ничего не открывает и ничего не скрывает.{' '}
          {group.broken}
        </Banner>
      )}
      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      <div className="page-section">
        <h3>Правило</h3>
        <div className="form-grid">
          <label className="form-row">
            <span className="fld-label">Название</span>
            <input
              className="fld-long"
              value={card.title}
              onChange={(e) => setCard({ ...card, title: e.target.value })}
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Зачем заведена</span>
            <textarea
              value={card.note}
              onChange={(e) => setCard({ ...card, note: e.target.value })}
            />
          </label>
          <RuleEditor rule={rule} onChange={setRule} />
          {canClients && (
            <div className="form-actions">
              <button onClick={save} disabled={busy}>
                Сохранить
              </button>
              <button onClick={count} disabled={busy}>
                Посчитать, сколько попадает
              </button>
              {size !== null && (
                <span className="hint">
                  Сейчас попадает {счётом(size, 'врач', 'врача', 'врачей')}.
                </span>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="page-section">
        <h3>Названные поимённо</h3>
        <p className="hint">
          Поимённый список не заменяет правило, а дополняет его: врач в группе,
          если подходит по правилу или назван здесь.
        </p>
        {loaded.members.length === 0 ? (
          <p className="empty">Поимённо никто не назван.</p>
        ) : (
          <ul className="units">
            {loaded.members.map((one) => (
              <li key={one.account} className="row-line">
                <span>
                  <span className="mono">№ {one.account}</span>{' '}
                  {one.email === '' ? 'почта не привязана' : one.email}
                </span>
                <span className="row-tools">
                  <span className="muted">
                    {one.addedBy === '' ? '' : `добавил ${one.addedBy}, `}
                    {датой(one.addedAt)}
                  </span>
                  {canClients && <button onClick={() => void drop(one.account)}>Убрать</button>}
                </span>
              </li>
            ))}
          </ul>
        )}
        {canClients && (
          <form className="form-actions page-section" onSubmit={add}>
            <label>
              <span className="visually-hidden">Номер врача</span>
              <input
                className="fld-short"
                value={adding}
                onChange={(e) => setAdding(e.target.value)}
                placeholder="№ врача"
              />
            </label>
            <button type="submit" disabled={busy}>
              Добавить врача
            </button>
          </form>
        )}
      </div>

      {canClients && (
        <div className="page-section">
          <h3>Убрать группу</h3>
          <p className="hint">
            Группу, которой открыт или от которой скрыт хоть один набор, убрать
            нельзя: удаление молча сменило бы состав задач у всех её членов.
          </p>
          <div className="form-actions">
            <button onClick={remove} disabled={busy}>
              Убрать группу
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
