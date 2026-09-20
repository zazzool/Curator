import { Fragment, useCallback, useState, type FormEvent } from 'react'

import { датой, датойИвременем } from './words'
import { dropDraft, readDraft, writeDraft } from './draftStore'
import { ПРАВА, правоСловами, праваСловами } from './permissions'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import type { CompactionGroup, CompactionPlan, Me, Prompt, Rule, RuleEdit, StudioUser } from './api'
import { Banner } from './components/Banner'

// Мастерская: пользователи студии, задания моделям и ключи программ.
//
// Всё это настройка службы, а не работа над задачами, и закрыто оно
// разными правами: задания правит тот, кто отвечает за качество
// генерации, ключи заводит тот, кто выкладывает сборки, а пользователей —
// тот, кому доверено раздавать доступ.

export function Workshop({ me }: { me: Me }) {
  return (
    <div className="stack">
      <Users me={me} />
      <Prompts me={me} />
      <Rules me={me} />
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

  const [draft, setDraft] = useState<{ login: string; displayName: string; permissions: string[] }>(
    { login: '', displayName: '', permissions: [] },
  )
  const [made, setMade] = useState<{ login: string; secret: string; note: string } | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [failure, setFailure] = useState('')

  const canWorkshop = me.permissions.includes('workshop')

  // Отказ чтения живёт в самом чтении, а не в общем `failure`: смешай их —
  // и отказ заведения входа гасился бы удачным перечитыванием списка,
  // которое идёт сразу за ним.
  const read = useCallback(async () => (await api.users())?.users ?? [], [])
  const users = useResource(read, 'Пользователи не прочитаны')
  const reload = users.reload

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
    const taking = user.permissions.includes(code)
    const next = taking
      ? user.permissions.filter((one) => one !== code)
      : [...user.permissions, code]
    // Флажок применяется сразу, без шага сохранения, и это остаётся так:
    // шаг сохранения на одиннадцати флажках — это одиннадцать поводов
    // забыть нажать. Но СНЯТИЕ права спрашивается: выдача исправляется
    // тем же флажком, а снятие человек замечает не здесь, а когда у него
    // пропал раздел, и объяснить это будет некому.
    if (
      taking &&
      !confirmed(
        `Снять право «${правоСловами(code)}» у «${user.login}»?`,
        'Раздел, который это право открывает, у него закроется сразу.',
      )
    ) {
      return
    }
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
    // Спрашивается только закрытие: открыть вход обратно можно той же
    // кнопкой, и подтверждение у обратимого приучает отвечать «да» не
    // читая — а вместе с ним перестают читать и остальные вопросы.
    if (
      disabled &&
      !confirmed(
        `Закрыть вход «${user.login}»${user.displayName ? ` (${user.displayName})` : ''}?`,
        'Войти в студию этим именем станет нельзя. Работа, сделанная им, остаётся на месте.',
      )
    ) {
      return
    }
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

      {failure && <Banner kind="error">{failure}</Banner>}

      {made && (
        <Banner kind="success">
          Вход «{made.login}» заведён. {made.note}
          <br />
          <span className="mono">{made.secret}</span>
        </Banner>
      )}

      <div className="page-section">
        <Loaded from={users}>
        {(list) => (
          // Заведённые входы — однородные записи с одними и теми же
          // столбцами, и читают их сравнением: у кого какие права, кто
          // когда заведён. Карточка заставляет сличать это глазами по
          // разным местам строки, столбец — нет. Так это устроено и у
          // донора.
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Вход</th>
                  <th>Имя</th>
                  <th>Права</th>
                  <th>Заведён</th>
                  {canWorkshop && <th />}
                </tr>
              </thead>
              <tbody>
                {list.map((user) => (
                  <Fragment key={user.login}>
                    <tr className={user.disabled ? 'faint' : undefined}>
                      <td>
                        <span className="mono">{user.login}</span>
                        {user.disabled && <span className="tag">вход закрыт</span>}
                      </td>
                      <td>{user.displayName}</td>
                      <td className="muted">{праваСловами(user.permissions)}</td>
                      <td className="muted">{датой(user.createdAt)}</td>
                      {canWorkshop && (
                        <td>
                          <span className="toolbar">
                            <button onClick={() => setOpen(open === user.login ? null : user.login)}>
                              {open === user.login ? 'Свернуть' : 'Права'}
                            </button>
                            {/* Красным только закрытие: открыть вход
                                обратно — не опасное действие. */}
                            <button
                              className={user.disabled ? undefined : 'danger'}
                              onClick={() => setDisabled(user, !user.disabled)}
                            >
                              {user.disabled ? 'Открыть вход' : 'Закрыть вход'}
                            </button>
                          </span>
                        </td>
                      )}
                    </tr>
                    {open === user.login && canWorkshop && (
                      // Правка прав раскрывается под своей же строкой и во
                      // всю ширину: одиннадцать прав с пояснениями в столбец
                      // не помещаются, а рядом с таблицей потеряли бы, чьи
                      // они.
                      <tr>
                        <td colSpan={5}>
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
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        )}
        </Loaded>
      </div>

      {canWorkshop && (
        <form className="page-section form-grid" onSubmit={create}>
          <h3 className="group-title">Завести вход</h3>
          <label className="form-row">
            <span className="fld-label">Имя входа</span>
            <input
              className="fld-medium"
              value={draft.login}
              onChange={(e) => setDraft({ ...draft, login: e.target.value })}
              placeholder="ivanova"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Как зовут</span>
            <input
              className="fld-long"
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
  const [open, setOpen] = useState<string | null>(null)
  const [draft, setDraft] = useState({ name: '', systemMd: '', userMd: '', revision: 0 })
  /** Когда был записан восстановленный черновик; пустая строка — своего нет. */
  const [restored, setRestored] = useState('')
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canEdit = me.permissions.includes('prompts')

  const read = useCallback(async () => (await api.prompts())?.prompts ?? [], [])
  const prompts = useResource(read, 'Задания моделей не прочитаны')
  const reload = prompts.reload

  type Набранное = { name: string; systemMd: string; userMd: string; revision: number }

  function edit(prompt: Prompt) {
    setOpen(prompt.id)
    setNote('')
    setFailure('')
    const сСервера: Набранное = {
      name: prompt.name,
      systemMd: prompt.systemMd,
      userMd: prompt.userMd,
      revision: prompt.revision,
    }

    // Черновик подхватывается, только если он набран на той же редакции,
    // что сейчас на сервере. Набранный на прежней означает, что задание
    // с тех пор правил кто-то другой: подставить его молча значит дать
    // человеку дописать поверх чужой работы, не показав её. Такой
    // черновик выбрасывается — сохранить его всё равно не дала бы сверка
    // редакций на сервере.
    const свой = readDraft<Набранное>(prompt.id)
    if (свой !== null && свой.revision === prompt.revision) {
      setDraft(свой.value)
      setRestored(свой.savedAt)
      return
    }
    if (свой !== null) dropDraft(prompt.id)
    setDraft(сСервера)
    setRestored('')
  }

  /**
   * Правит черновик и тут же записывает его в браузер.
   *
   * Запись идёт на каждое нажатие клавиши, без задержки: задержка
   * спасает от частой записи, а теряется набранное именно в последние
   * секунды — на закрытой вкладке или уснувшей машине, когда отложенная
   * запись не случится уже никогда. Пишется одна строка в localStorage,
   * и цена этого не измерима рядом с ценой потери.
   */
  function правим(next: Набранное) {
    setDraft(next)
    if (open !== null) writeDraft(open, next, next.revision)
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
      // Сохранённое на сервере больше не черновик: оставленный, он
      // восстановился бы при следующем заходе и выглядел бы как
      // несохранённое, которого нет.
      dropDraft(open)
      setDraft({ ...draft, revision: out.revision })
      setRestored('')
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
    <div className="page-section">
      <div className="page-head">
        <h2>Задания моделям</h2>
      </div>
      <p className="hint">
        Тем, что здесь написано, модель и пишет задачи. Правка меняет всё,
        что будет сгенерировано дальше, но не трогает написанного раньше.
      </p>
      {!canEdit && <p className="hint">Задания правит тот, кому выдано право «задания».</p>}

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      <div className="page-section">
        <Loaded from={prompts}>
        {(list) => list.length === 0 ? (
          <p className="empty">Заданий нет: генерация не запустится.</p>
        ) : (
          <div className="list">
            {list.map((prompt) => (
              <button
                key={prompt.id}
                className="list-row"
                onClick={() => (open === prompt.id ? setOpen(null) : edit(prompt))}
              >
                <span>
                  {prompt.name}
                  <span className="tag">{prompt.nodeWord || prompt.node}</span>
                </span>
                <span className="muted">редакция {prompt.revision}</span>
              </button>
            ))}
          </div>
        )}
        </Loaded>
      </div>

      {open !== null && canEdit && (
        <form className="page-section form-grid" onSubmit={save}>
          <label className="form-row">
            <span className="fld-label">Как зовётся</span>
            <input
              className="fld-long"
              value={draft.name}
              onChange={(e) => правим({ ...draft, name: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Что модель знает о себе</span>
            <textarea
              value={draft.systemMd}
              onChange={(e) => правим({ ...draft, systemMd: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Что ей поручается</span>
            <textarea
              value={draft.userMd}
              onChange={(e) => правим({ ...draft, userMd: e.target.value })}
            />
          </label>
          {restored !== '' && (
            <Banner kind="info">
              Восстановлено несохранённое от {датойИвременем(restored)}. Сохранённое
              на сервере не тронуто — нажмите «Вернуть сохранённое», если
              этот черновик не нужен.
            </Banner>
          )}
          <p className="hint">
            Редакция сверяется при сохранении: двое, открывшие одно задание,
            иначе затрут друг друга молча — и узнает об этом тот, чья правка
            пропала, по качеству задач через неделю.
          </p>
          <div className="form-actions">
            <button type="submit" disabled={busy}>
              Сохранить задание
            </button>
            {restored !== '' && (
              <button
                type="button"
                onClick={() => {
                  // Выбрасывать черновик — дело человека, а не студии:
                  // восстановленное он мог не узнать в лицо, и пути
                  // обратно к сохранённому без этой кнопки нет.
                  const было =
                    prompts.state === 'ready'
                      ? prompts.value.find((one) => one.id === open)
                      : undefined
                  if (было) {
                    dropDraft(было.id)
                    setDraft({
                      name: было.name,
                      systemMd: было.systemMd,
                      userMd: было.userMd,
                      revision: было.revision,
                    })
                    setRestored('')
                  }
                }}
              >
                Вернуть сохранённое
              </button>
            )}
            <button type="button" onClick={() => setOpen(null)}>
              Закрыть
            </button>
          </div>
        </form>
      )}
    </div>
  )
}

// Свод правил: чему конвейер научился и чего от него требуют.
//
// Это единственное место, где видно самообучение. Правило, выведенное из
// замечаний детектора, копится молча и до кворума в задание не уходит —
// без этого экрана составитель узнал бы о нём только по тому, что задачи
// однажды изменились.
//
// Подтверждения показаны числом рядом с кворумом намеренно. «Кандидат»
// без числа читается как «сломалось»; «2 из 3» читается как «копится», и
// это разные новости.
function Rules({ me }: { me: Me }) {
  const [open, setOpen] = useState<string | null>(null)
  const [draft, setDraft] = useState<RuleEdit>(пустоеПравило())
  // Проверка открытого правила словами, как её написал сервер. Держится
  // отдельно от draft намеренно: draft — это то, что уедет обратно, а
  // проверка отсюда не правится, и положи мы её туда, первая же отправка
  // повезла бы поле, которого ручка не ждёт.
  const [checkWords, setCheckWords] = useState('')
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canEdit = me.permissions.includes('prompts')

  const read = useCallback(async () => (await api.rules())?.rules ?? [], [])
  const rules = useResource(read, 'Свод правил не прочитан')
  const reload = rules.reload

  function edit(rule: Rule) {
    setOpen(rule.id)
    setNote('')
    setFailure('')
    setCheckWords(rule.checkWords ?? '')
    setDraft({
      title: rule.title,
      text: rule.text,
      why: rule.why,
      kind: rule.kind,
      status: rule.status,
      scope: rule.scope ?? {},
    })
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      if (open === 'новое') {
        const made = await api.createRule(draft)
        setNote(`Правило «${made.title}» записано и действует.`)
      } else if (open !== null) {
        await api.saveRule(open, draft)
        setNote('Правило сохранено.')
      }
      await reload()
      setOpen(null)
    } catch (error) {
      // Текст отказа берётся у сервера: выверка правила живёт там, и
      // своё «не удалось сохранить» отправило бы человека нажимать ту же
      // кнопку снова, не сказав, что именно не так.
      setFailure(error instanceof ApiError ? error.message : 'Правило не сохранено')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page-section">
      <div className="page-head">
        <h2>Свод правил</h2>
        {canEdit && (
          <button
            type="button"
            onClick={() => {
              setOpen('новое')
              setDraft(пустоеПравило())
              setCheckWords('')
              setNote('')
              setFailure('')
            }}
          >
            Написать правило
          </button>
        )}
      </div>
      <p className="hint">
        Правила уходят в задание модели вместе с заказом. Написанное здесь
        действует сразу; выведенное из замечаний проверок ждёт трёх разных
        задач — одна ошибка случайность, две совпадение.
      </p>
      {!canEdit && <p className="hint">Свод правит тот, кому выдано право «задания».</p>}

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      <div className="page-section">
        <Loaded from={rules}>
          {(list) =>
            list.length === 0 ? (
              <p className="empty">Свод пуст: конвейер пишет задачи без накопленных правил.</p>
            ) : (
              <div className="list">
                {list.map((rule) => (
                  <button
                    key={rule.id}
                    className="list-row"
                    onClick={() => (open === rule.id ? setOpen(null) : edit(rule))}
                  >
                    <span>
                      {rule.title}
                      <span className="tag">{rule.kindWord}</span>
                      <span className="tag">{откуда(rule.source)}</span>
                      {/*
                        Правило с машинной проверкой помечено отдельно, и
                        пометка эта не украшение: такое правило обязательно
                        к исполнению, а правило без неё держится тем,
                        согласится ли модель. Решая, чинить задачу или
                        правило, составитель должен знать, какое из двух
                        перед ним.
                      */}
                      {rule.check && <span className="tag">проверяется машиной</span>}
                    </span>
                    <span className="muted">{состояниеСловами(rule)}</span>
                  </button>
                ))}
              </div>
            )
          }
        </Loaded>
      </div>

      {canEdit && (
        <Compaction
          rules={rules.state === 'ready' ? rules.value : []}
          onApplied={reload}
        />
      )}

      {open !== null && canEdit && (
        <form className="page-section form-grid" onSubmit={save}>
          <label className="form-row">
            <span className="fld-label">Как зовётся</span>
            <input
              className="fld-long"
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Что требуется от модели</span>
            <textarea
              value={draft.text}
              onChange={(e) => setDraft({ ...draft, text: e.target.value })}
            />
          </label>
          <label>
            <span className="fld-label">Почему это правило есть</span>
            <textarea
              value={draft.why}
              onChange={(e) => setDraft({ ...draft, why: e.target.value })}
            />
          </label>
          <label className="form-row">
            <span className="fld-label">О чём правило</span>
            <select value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}>
              {РОДА.map((one) => (
                <option key={one.code} value={one.code}>
                  {one.word}
                </option>
              ))}
            </select>
          </label>
          {open !== 'новое' && (
            <label className="form-row">
              <span className="fld-label">Что с ним делать</span>
              <select
                value={draft.status ?? ''}
                onChange={(e) => setDraft({ ...draft, status: e.target.value })}
              >
                {/*
                  Нынешнее состояние стоит в списке своим пунктом, когда
                  выбрать его рукой нельзя. Без этого список показывал бы
                  «действует» у правила, которое копится: пустой выбор
                  браузер рисует первым пунктом, и составитель читал бы
                  чужое состояние как своё.
                */}
                {draft.status !== 'active' && draft.status !== 'muted' && (
                  <option value={draft.status}>{словоСостояния(draft.status ?? '')}</option>
                )}
                <option value="active">действует</option>
                <option value="muted">погашено</option>
              </select>
            </label>
          )}
          {/*
            Проверка показана, но не правится, и сказано это прямо. Поле,
            которое выглядит полем, а сохраняется мимо, хуже отсутствующего:
            составитель написал бы проверку и считал бы, что она стоит.
          */}
          {open !== 'новое' && checkWords !== '' && (
            <p className="hint">
              Это правило проверяется машинно: {checkWords}. Проверка переписывается
              пока не здесь — правка правила её не тронет и не сотрёт.
            </p>
          )}
          <p className="hint">
            Погашенное правило не удаляется и не возвращается само: подтверждения
            проверок его больше не воскресят. Свод дрейфует, и вопрос «чего мы
            требовали в марте» должен иметь ответ.
          </p>
          <div className="form-actions">
            <button type="submit" disabled={busy}>
              {open === 'новое' ? 'Записать правило' : 'Сохранить правило'}
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

/**
 * Уплотнение свода: слить сказанное дважды, сжать многословное.
 *
 * Панель показывается всегда, а не только когда свод перерос блок, и это
 * решение. Место в блоке кончается МОЛЧА — правило, не поместившееся в
 * него, выглядит действующим и не действует, — и узнать об этом
 * составитель может только здесь. Спрячь панель до беды, и он увидит её
 * ровно тогда, когда беда уже случилась и держится неделю.
 *
 * План предлагается отдельно от применения намеренно: уплотнение меняет
 * то, что уходит модели, то есть качество всех будущих задач. Между
 * предложением и сводом стоит человек, и он снимает галочки с групп,
 * которые не принял.
 */
function Compaction({ rules, onApplied }: { rules: Rule[]; onApplied: () => Promise<unknown> }) {
  const [plan, setPlan] = useState<CompactionPlan | null>(null)
  const [taken, setTaken] = useState<Record<string, boolean>>({})
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  // Сколько правил уходит в задание — считаем по тому же признаку, что и
  // сервер: действующее и не закрытое. Число «правил в своде» здесь
  // соврало бы: погашенные и слитые в блок не идут.
  const живых = rules.filter((r) => r.status === 'active' && !r.validTo).length

  async function suggest() {
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      const got = await api.suggestCompaction()
      setPlan(got)
      // Все группы отмечены заранее: план и так прошёл заслон сервера, а
      // пустой список галочек заставил бы отмечать по одной то, что
      // составитель чаще всего принимает целиком.
      setTaken(Object.fromEntries((got?.groups ?? []).map((g) => [g.keepId, true])))
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'План уплотнения не составлен')
    } finally {
      setBusy(false)
    }
  }

  async function apply() {
    const groups = (plan?.groups ?? []).filter((g) => taken[g.keepId])
    if (groups.length === 0) {
      return
    }
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      const done = await api.applyCompaction(groups as CompactionGroup[])
      // «Просили» и «прошло» называются парой. Скажи мы одно «слито 3»,
      // и составитель, пославший пять групп, решил бы, что свод уплотнён
      // целиком, — а две отверг заслон.
      const хвост =
        done.merged === done.asked
          ? ''
          : ` Отвергнуто заслоном: ${done.asked - done.merged}.`
      setNote(
        `Слито групп: ${done.merged} из ${done.asked}.` +
          хвост +
          ` Блок правил: ${done.before} → ${done.after} байт из ${done.limit}.`,
      )
      setPlan(null)
      await onApplied()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Уплотнение не применено')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page-section">
      <div className="page-head">
        <h3>Уплотнение свода</h3>
        <button type="button" onClick={suggest} disabled={busy}>
          Предложить, что слить
        </button>
      </div>
      <p className="hint">
        Свод растёт сам, и правила приходят к одному и тому же разными словами.
        Место в задании ограничено: то, что не поместилось, выглядит действующим
        и до модели не доезжает. Сейчас в задание уходит {живых} правил.
      </p>

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      {plan && (
        <>
          <Banner kind={plan.dropped > 0 ? 'error' : 'info'}>
            {plan.dropped > 0
              ? `До модели не доезжает правил: ${plan.dropped}. Блок вмещает ${plan.limit} байт, правила занимают ${plan.before}.`
              : `Блок вмещает ${plan.limit} байт, правила занимают ${plan.before}. Всё доезжает.`}
          </Banner>
          {plan.note && <p className="hint">{plan.note}</p>}
          {plan.groups.length === 0 ? (
            <p className="empty">Сливать нечего: правила говорят о разном.</p>
          ) : (
            <>
              <div className="list">
                {plan.groups.map((group) => (
                  <label key={group.keepId} className="list-row">
                    <span>
                      <input
                        type="checkbox"
                        checked={taken[group.keepId] ?? false}
                        onChange={(e) =>
                          setTaken({ ...taken, [group.keepId]: e.target.checked })
                        }
                      />{' '}
                      {group.text}
                      <span className="tag">
                        {group.mergeIds.length === 0
                          ? 'сжать'
                          : `слить ${group.mergeIds.length + 1}`}
                      </span>
                      {/*
                        Обоснование стоит рядом с текстом, а не прячется:
                        по нему составитель и решает. «Оба про слова из
                        положений» проверяемо, «похожи» — нет, и увидеть
                        разницу он должен до того, как нажмёт.
                      */}
                      <span className="muted"> {group.why}</span>
                    </span>
                    <span className="muted">освободит {group.saved} б.</span>
                  </label>
                ))}
              </div>
              <div className="form-actions">
                <button type="button" onClick={apply} disabled={busy}>
                  Слить отмеченное
                </button>
                <button type="button" onClick={() => setPlan(null)} disabled={busy}>
                  Не сливать
                </button>
              </div>
              <p className="hint">
                Слитое правило не удаляется: оно закрывается датой со ссылкой на
                то, в которое слито. Подтверждения проверок после этого доезжают
                до выжившего, а не копятся у закрытого.
              </p>
            </>
          )}
        </>
      )}
    </div>
  )
}

/** Рода правил. Порядок — по весу в задании: существо впереди слога. */
const РОДА = [
  { code: 'substance', word: 'существо' },
  { code: 'consistency', word: 'согласованность' },
  { code: 'marking', word: 'разметка' },
  { code: 'structure', word: 'устройство' },
  { code: 'language', word: 'слог' },
]

function пустоеПравило(): RuleEdit {
  return { title: '', text: '', why: '', kind: 'substance', scope: {} }
}

function откуда(source: string): string {
  switch (source) {
    case 'curator':
      return 'написано вами'
    case 'builtin':
      return 'встроенное'
    case 'edit':
      return 'из правки'
    case 'lint':
      return 'из замечаний'
    default:
      return source
  }
}

/**
 * Состояние правила словами и числом.
 *
 * «Кандидат» без числа читается как «сломалось». Число подтверждений
 * рядом с кворумом читается как «копится», и это разные новости: во
 * втором случае делать ничего не надо.
 */
function состояниеСловами(rule: Rule): string {
  if (rule.status === 'candidate') {
    return `копится: ${rule.confirmations} из ${rule.quorum}`
  }
  return словоСостояния(rule.status)
}

function словоСостояния(status: string): string {
  switch (status) {
    case 'active':
      return 'действует'
    case 'candidate':
      return 'копится'
    case 'muted':
      return 'погашено'
    case 'draft':
      return 'черновик'
    case 'deprecated':
      return 'закрыто'
    case 'merged':
      return 'слито'
    default:
      return status
  }
}

// Ключи программ.
function AppKeys({ me }: { me: Me }) {
  const [draft, setDraft] = useState({ keyId: '', title: '' })
  const [issued, setIssued] = useState<{ keyId: string; key: string; note: string } | null>(null)
  const [failure, setFailure] = useState('')

  const canWorkshop = me.permissions.includes('workshop')

  const read = useCallback(async () => (await api.appKeys())?.keys ?? [], [])
  const keys = useResource(read, 'Ключи программ не прочитаны')
  const reload = keys.reload

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
    // Самое дорогое действие в этом разделе, и по виду кнопки этого
    // прежде было не понять: ключ зашит в СОБРАННЫЕ сборки, стоящие у
    // врачей на телефонах. Закрыть его — значит отрезать их все разом, и
    // починка займёт столько, сколько занимает выкладка в магазин.
    if (
      !confirmed(
        `Закрыть ключ программы «${keyId}»?`,
        'Все уже поставленные сборки с этим ключом перестанут подключаться к серверу. ' +
          'Починить это можно только новой выкладкой в магазин.',
      )
    ) {
      return
    }
    try {
      await api.disableAppKey(keyId)
      await reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Ключ не закрыт')
    }
  }

  return (
    <div className="page-section">
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

      {failure && <Banner kind="error">{failure}</Banner>}

      {issued && (
        <Banner kind="success">
          Ключ {issued.keyId} заведён. {issued.note}
          <br />
          <span className="mono">{issued.key}</span>
        </Banner>
      )}

      <div className="page-section">
        <Loaded from={keys}>
        {(list) => list.length === 0 ? (
          <p className="empty">Ключей нет: ни одна сборка приложения к серверу не подключится.</p>
        ) : (
          <div className="list">
            {list.map((key) => (
              <div key={key.keyId} className="list-row">
                <span>
                  <span className="mono">{key.keyId}</span> {key.title}
                  {key.disabled && <span className="tag">закрыт</span>}
                </span>
                <span className="row-tools">
                  <span className="muted">заведён {датой(key.createdAt)}</span>
                  {canWorkshop && !key.disabled && (
                    <button className="danger" onClick={() => disable(key.keyId)}>
                      Закрыть
                    </button>
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
        </Loaded>
      </div>

      {canWorkshop && (
        <form className="page-section form-grid" onSubmit={issue}>
          <label className="form-row">
            <span className="fld-label">Опознаватель</span>
            <input
              className="fld-medium"
              value={draft.keyId}
              onChange={(e) => setDraft({ ...draft, keyId: e.target.value })}
              placeholder="android-2026-09"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Для чего</span>
            <input
              className="fld-long"
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
