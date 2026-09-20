import { Fragment, useCallback, useState, type FormEvent } from 'react'

import { датой, датойИвременем } from './words'
import { dropDraft, readDraft, writeDraft } from './draftStore'
import { ПРАВА, правоСловами, праваСловами } from './permissions'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import type {
  CheckCatalog,
  CompactionGroup,
  CompactionPlan,
  Me,
  PipelineNode,
  Prompt,
  Rule,
  RuleCheck,
  RuleEdit,
  StudioUser,
} from './api'
import { долларами } from './Money'
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
      <Pipeline me={me} />
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
/**
 * Конвейер целиком: узлы по порядку, у каждого своя модель и своя цена.
 *
 * Отдельно от списка заданий, хотя настройка у них одна, потому что
 * вопросы разные. Список заданий отвечает «что написано этому узлу»;
 * конвейер — «во что обходится работа и где её менять». Второй вопрос
 * задают обо ВСЁМ конвейере сразу, и, открывая узлы по одному, на него
 * не ответить.
 *
 * Перечень узлов приходит с сервера, а не повторяется здесь: повторённый
 * разошёлся бы молча на том узле, который добавили последним, и экран
 * показал бы конвейер короче настоящего — не поломкой, а конвейером
 * покороче.
 */
function Pipeline({ me }: { me: Me }) {
  // Чей конвейер показан. Ноль — общий: он и работает там, где источник
  // своего задания не завёл.
  const [источник, setИсточник] = useState(0)
  const [отказ, setОтказ] = useState('')
  const [занят, setЗанят] = useState(false)

  const read = useCallback(async () => await api.pipeline(источник), [источник])
  const конвейер = useResource(read, 'Конвейер не прочитан')
  const reload = конвейер.reload

  const readSources = useCallback(async () => (await api.sources())?.sources ?? [], [])
  const источники = useResource(readSources, '')

  async function своё(node: string, было: boolean) {
    setОтказ('')
    setЗанят(true)
    try {
      if (было) {
        await api.unforkNodePrompt(источник, node)
      } else {
        await api.forkNodePrompt(источник, node)
      }
      await reload()
    } catch (error) {
      // Текст сервера как есть: он пишет по-русски и говорит, чего не
      // хватает. Своё «не удалось» отправило бы человека нажимать ту же
      // кнопку снова.
      setОтказ(error instanceof ApiError ? error.message : 'Задание узла не изменено')
    } finally {
      setЗанят(false)
    }
  }

  if (!me.permissions.includes('prompts')) return null

  return (
    <div className="page-section">
      <div className="page-head">
        <h2>Конвейер</h2>
      </div>
      <label className="form-row">
        <span className="fld-label">Чей конвейер</span>
        <select
          className="fld-long"
          value={источник}
          onChange={(e) => setИсточник(Number(e.target.value))}
        >
          {/*
            «Общий» стоит первым и выбран сразу: им работают все
            источники, кроме тех, кому завели своё. Открой мы экран на
            чьём-то конвейере, составитель правил бы задание одного
            источника, думая, что правит общее.
          */}
          <option value={0}>Общий — им работают все</option>
          {(источники.state === 'ready' ? источники.value : []).map((one) => (
            <option key={one.id} value={one.id}>
              {one.title}
            </option>
          ))}
        </select>
      </label>
      {отказ && <Banner kind="error">{отказ}</Banner>}
      <Loaded from={конвейер}>
        {(сведения) => (
          <>
            <p className="hint">
              Узлы идут в том порядке, в каком через них проходит задача.
              Числа — за {сведения.days} суток; модель узла меняют, и числа
              за всё время смешали бы работу прежней модели с работой
              нынешней.
            </p>
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Узел</th>
                    <th>Модель</th>
                    <th>Обращений</th>
                    <th>Отказов</th>
                    <th>Цена обращения</th>
                    <th>Время</th>
                    {источник > 0 && <th>Задание</th>}
                  </tr>
                </thead>
                <tbody>
                  {сведения.nodes.map((узел, место) => (
                    <tr key={узел.node}>
                      <td>
                        {место + 1}. {узел.word}
                        {/*
                          Узел без задания не работает вовсе, и сказать об
                          этом надо здесь: пропусти мы строку, пропажа
                          задания выглядела бы как пропажа узла, а искать
                          её пошли бы в коде.
                        */}
                        {узел.promptId === '' && (
                          <span className="tag">задания нет</span>
                        )}
                      </td>
                      <td>{узел.model || <span className="muted">модель поставщика</span>}</td>
                      <td>{узел.calls}</td>
                      <td>{узел.failed > 0 ? узел.failed : <span className="muted">—</span>}</td>
                      <td>
                        <ЦенаУзла узел={узел} />
                      </td>
                      <td>
                        {узел.calls === 0 ? (
                          <span className="muted">—</span>
                        ) : (
                          `${узел.medianMs} мс`
                        )}
                      </td>
                      {источник > 0 && (
                        <td>
                          {/*
                            Состояние названо словом, а кнопка — тем, что
                            она сделает. Кнопка, подписанная состоянием
                            («своё»), читается как «уже своё», и нажимают
                            её именно те, кто хотел вернуть общее.
                          */}
                          <button
                            type="button"
                            disabled={занят}
                            onClick={() => void своё(узел.node, узел.own)}
                          >
                            {узел.own ? 'Вернуть общее' : 'Завести своё'}
                          </button>
                          {узел.own && <span className="tag">своё</span>}
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {источник > 0 && (
              <p className="hint">
                Своё задание — копия общего, которую дальше правят отдельно:
                разбор приказа и разбор клинических рекомендаций читают
                по-разному. «Вернуть общее» снимает только привязку —
                правленое задание остаётся в базе, и «Завести своё»
                находит его снова.
              </p>
            )}
            <p className="hint">
              Цена — медианная за одно состоявшееся обращение, а не средняя:
              одно обращение разбора документа стоит десятка обращений
              сверки, и среднее по узлу, куда попал один длинный документ,
              показало бы дорогим узел, который дорог не был.
            </p>
          </>
        )}
      </Loaded>
    </div>
  )
}

/**
 * Цена узла с оговоркой, если она оценка.
 *
 * Оценка не выдаётся за факт: цену обращения называет маршрутизатор, а
 * остальным шлюзам её считают по прайсу, и расчёт не знает ни скидки
 * префиксного кэша, ни наценки шлюза — там, где кэш работает, он
 * ошибается в разы. Число, которое выглядит точным и таковым не
 * является, хуже отсутствия числа.
 */
function ЦенаУзла({ узел }: { узел: PipelineNode }) {
  // Считается по СОСТОЯВШИМСЯ, и потому «обращений не было» и «ни одно
  // не состоялось» — один и тот же случай для цены и разные для узла.
  // Отказ чаще всего не стоит ничего, и узел, у которого отказали все
  // обращения, показал бы «$0» — то есть «бесплатный узел» там, где он
  // просто не работал ни разу.
  const состоялось = узел.calls - узел.failed
  if (состоялось <= 0) {
    return (
      <span className="muted">
        {узел.calls === 0 ? 'обращений не было' : 'ни одно не состоялось'}
      </span>
    )
  }
  return (
    <>
      {долларами(узел.medianNanoUsd)}
      {узел.estimated > 0 && (
        <span className="tag">
          оценка у {узел.estimated} из {состоялось}
        </span>
      )}
    </>
  )
}

function Prompts({ me }: { me: Me }) {
  const [open, setOpen] = useState<string | null>(null)
  const [draft, setDraft] = useState({ name: '', systemMd: '', userMd: '', model: '', revision: 0 })
  /** Когда был записан восстановленный черновик; пустая строка — своего нет. */
  const [restored, setRestored] = useState('')
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canEdit = me.permissions.includes('prompts')

  const read = useCallback(async () => (await api.prompts())?.prompts ?? [], [])
  const prompts = useResource(read, 'Задания моделей не прочитаны')
  const reload = prompts.reload

  // Список моделей — ПОДСКАЗКА, а не словарь. Новая модель появляется у
  // поставщика раньше, чем в нашем прайсе, и список выбора отказывал бы
  // ровно в тот день, когда её понадобилось попробовать. Поэтому поле
  // остаётся вводом, а список висит на нём datalist'ом.
  //
  // Отказ чтения списка не прячет поле и ничего не говорит: без подсказки
  // модель вписывается руками, а полоса отказа рядом с работающим полем
  // отправила бы составителя чинить то, что не сломано.
  const readModels = useCallback(async () => (await api.models())?.models ?? [], [])
  const models = useResource(readModels, '')

  type Набранное = {
    name: string
    systemMd: string
    userMd: string
    model: string
    revision: number
  }

  function edit(prompt: Prompt) {
    setOpen(prompt.id)
    setNote('')
    setFailure('')
    const сСервера: Набранное = {
      name: prompt.name,
      systemMd: prompt.systemMd,
      userMd: prompt.userMd,
      model: prompt.model,
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
                <span className="muted">
                  {/*
                    Модель названа в самой строке, а не только в открытой
                    форме: узлов больше пяти, и «какой узел какой моделью»
                    — это вопрос обо ВСЁМ конвейере сразу. Открывая их по
                    одному, ответить на него нельзя.
                  */}
                  {prompt.model || 'модель поставщика'} · редакция {prompt.revision}
                </span>
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
          <label className="form-row">
            <span className="fld-label">Какой моделью</span>
            <input
              className="fld-medium"
              list="модели-прайса"
              placeholder="моделью поставщика"
              value={draft.model}
              onChange={(e) => правим({ ...draft, model: e.target.value })}
            />
          </label>
          <datalist id="модели-прайса">
            {(models.state === 'ready' ? models.value : []).map((one) => (
              <option key={`${one.provider}/${one.model}`} value={one.model}>
                {one.provider}
              </option>
            ))}
          </datalist>
          <p className="hint fld-across">
            Пусто — моделью поставщика. Узлы стоят разных денег и требуют
            разного: написание условия — самая дорогая работа конвейера, а
            слепая сверка отвечает одним словом из списка.
          </p>
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
                      model: было.model,
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
  // Проверка открытого правила словами, как её написал сервер. Фраза
  // считается ТАМ, и второй её сборки здесь нет: собери мы её в студии —
  // составитель читал бы в списке одно, а в форме другое, разойдясь на
  // первой же правке формулировки.
  const [checkWords, setCheckWords] = useState('')
  // Проверка, какой её правят, и признак «трогали ли».
  //
  // Признак нужен затем, что у поля на сервере ТРИ состояния: не слали —
  // не трогали, слали null — снимают, слали запись — ставят. Шли студия
  // проверку всегда, правка одной запятой в тексте переписывала бы
  // проверку тем, что студия успела прочитать, — а прочитать она могла и
  // устаревшее.
  const [проверка, setПроверка] = useState<RuleCheck | null>(null)
  const [проверкуТрогали, setПроверкуТрогали] = useState(false)

  const readChecks = useCallback(async () => await api.checkCatalog(), [])
  const каталог = useResource(readChecks, 'Каталог проверок не прочитан')
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
    setПроверка(rule.check ?? null)
    setПроверкуТрогали(false)
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
      // Поле проверки едет, только если её трогали: нетронутую сервер
      // оставляет как была.
      const тело: RuleEdit = проверкуТрогали ? { ...draft, check: проверка } : draft
      if (open === 'новое') {
        const made = await api.createRule(тело)
        setNote(`Правило «${made.title}» записано и действует.`)
      } else if (open !== null) {
        await api.saveRule(open, тело)
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
              setПроверка(null)
              setПроверкуТрогали(false)
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
            Нынешняя проверка словами — как её написал сервер, а не как
            её собрала бы студия. Стоит НАД формой: открывший правило
            сперва читает, что оно меряет сейчас, и только потом решает,
            менять ли.
          */}
          {open !== 'новое' && checkWords !== '' && !проверкуТрогали && (
            <p className="hint">Сейчас это правило меряется так: {checkWords}.</p>
          )}
          <Loaded from={каталог}>
            {(catalog) => (
              <ПроверкаПравила
                catalog={catalog}
                check={проверка}
                onChange={(next) => {
                  setПроверка(next)
                  setПроверкуТрогали(true)
                }}
              />
            )}
          </Loaded>
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

/**
 * Форма машинной проверки правила.
 *
 * # Почему форма, а не поле для записи
 *
 * Каталог предикатов закрыт и живёт в коде сервера. Свободное поле
 * отдало бы серверу предикат, которого нет, и человек узнал бы об этом
 * после заполнения всей формы. Списками выбора он выбирает из того, что
 * исполнитель умеет, — и промахнуться не может.
 *
 * # Почему поля показываются не все
 *
 * У каждого предиката свои доводы, и сервер называет их сам (`params`).
 * Покажи мы все девять полей всегда — составитель заполнял бы «сколько
 * знаков» у запрета слов, а оно не читается никем: поле, которое
 * выглядит полем и никуда не идёт, хуже отсутствующего.
 *
 * # Почему фразы «вот что получится» здесь нет
 *
 * Фразу пишет сервер (`checkWords`), и второй её сборки в студии быть не
 * должно: собранная здесь, она разошлась бы с тем, что исполняет код, —
 * и составитель читал бы обещание, а меряло бы другое. После сохранения
 * фраза приезжает с сервера и встаёт над формой.
 */
function ПроверкаПравила({
  catalog,
  check,
  onChange,
}: {
  catalog: CheckCatalog
  check: RuleCheck | null
  onChange: (next: RuleCheck | null) => void
}) {
  const spec = catalog.checks.find((one) => one.type === check?.type)
  const берёт = (param: string) => spec?.params.includes(param) ?? false

  function правка(patch: Partial<RuleCheck>) {
    if (!check) return
    onChange({ ...check, ...patch })
  }

  return (
    <>
      <label className="form-row">
        <span className="fld-label">Проверяется машинно</span>
        <select
          className="fld-long"
          value={check?.type ?? ''}
          onChange={(e) => {
            const type = e.target.value
            // Смена предиката НЕ тащит доводы прежнего: «слова» у
            // запрета и «выражение» у выражения — разные вещи, а
            // сохранившееся поле уехало бы на сервер незаметно для
            // того, кто его не заполнял.
            onChange(type === '' ? null : { type })
          }}
        >
          {/*
            «Без проверки» стоит первым и выбран у правила, которое ею не
            меряется: это самый частый случай, и правило без проверки —
            законное правило, а не недоделанное.
          */}
          <option value="">без проверки — правило держится текстом задания</option>
          {catalog.checks.map((one) => (
            <option key={one.type} value={one.type}>
              {one.title}
            </option>
          ))}
        </select>
      </label>
      {spec && (
        <p className="hint">
          {spec.about}
          {spec.doctorOnly && '. Этот предикат модель не получает: пишете его только вы'}
        </p>
      )}
      {берёт('when') && (
        <label>
          <span className="fld-label">При каких словах запрет действует</span>
          <textarea
            value={(check?.when ?? []).join(', ')}
            onChange={(e) => правка({ when: словами(e.target.value) })}
          />
        </label>
      )}
      {берёт('words') && (
        <label>
          <span className="fld-label">
            {check?.type === 'require-words' ? 'Слова, одно из которых обязано быть' : 'Слова, которых быть не должно'}
          </span>
          {/*
            Через запятую, и сказано это прямо: набранное в столбик тоже
            принимается, но догадываться о разделителе составитель не
            должен.
          */}
          <textarea
            value={(check?.words ?? []).join(', ')}
            onChange={(e) => правка({ words: словами(e.target.value) })}
          />
        </label>
      )}
      {берёт('pattern') && (
        <label className="form-row">
          <span className="fld-label">Выражение</span>
          <input
            className="fld-long"
            value={check?.pattern ?? ''}
            onChange={(e) => правка({ pattern: e.target.value })}
          />
        </label>
      )}
      {берёт('what') && (
        <label className="form-row">
          <span className="fld-label">Что считать</span>
          <select
            className="fld-medium"
            value={check?.what ?? catalog.what[0]?.value ?? ''}
            onChange={(e) => правка({ what: e.target.value })}
          >
            {catalog.what.map((one) => (
              <option key={one.value} value={one.value}>
                {one.word}
              </option>
            ))}
          </select>
        </label>
      )}
      {берёт('where') && (
        <>
          <p className="hint">
            {/*
              Ничего не отмечено — фрагменты условия, и так же считает
              исполнитель. Сказано словами, а не подставлено галочкой:
              подставленная галочка означала бы выбор, которого человек не
              делал, и сняв её, он получил бы ровно то же самое.
            */}
            Где смотреть. Ничего не отмечено — смотрит во фрагменты условия.
          </p>
          <ul className="units">
            {catalog.where.map((one) => (
              <li key={one.value}>
                <label>
                  <input
                    type="checkbox"
                    checked={(check?.where ?? []).includes(one.value)}
                    onChange={(e) => {
                      const было = check?.where ?? []
                      правка({
                        where: e.target.checked
                          ? [...было, one.value]
                          : было.filter((f) => f !== one.value),
                      })
                    }}
                  />{' '}
                  {one.word}
                </label>
              </li>
            ))}
          </ul>
        </>
      )}
      {берёт('min') && (
        <label className="form-row">
          <span className="fld-label">Не меньше</span>
          <input
            className="fld-num"
            inputMode="numeric"
            value={check?.min ? String(check.min) : ''}
            onChange={(e) => правка({ min: числом(e.target.value) })}
          />
        </label>
      )}
      {берёт('max') && (
        <label className="form-row">
          <span className="fld-label">Не больше</span>
          <input
            className="fld-num"
            inputMode="numeric"
            value={check?.max ? String(check.max) : ''}
            onChange={(e) => правка({ max: числом(e.target.value) })}
          />
        </label>
      )}
      {берёт('gender') && (
        <label className="form-row">
          <span className="fld-label">Только когда</span>
          <select
            className="fld-medium"
            value={check?.gender ?? ''}
            onChange={(e) => правка({ gender: e.target.value })}
          >
            <option value="">условие о ком угодно</option>
            {catalog.gender.map((one) => (
              <option key={one.value} value={one.value}>
                {one.word}
              </option>
            ))}
          </select>
        </label>
      )}
      {берёт('allowNegated') && (
        <ul className="units">
          <li>
            <label>
              <input
                type="checkbox"
                checked={check?.allowNegated ?? false}
                onChange={(e) => правка({ allowNegated: e.target.checked })}
              />{' '}
              {/*
                «Отрицание не нарушение» — не тонкость: «алкоголь не
                употребляет» это обязательная запись, а не упоминание
                алкоголя, и запрет, считающий её нарушением, чинил бы
                исправные задачи.
              */}
              Отрицание нарушением не считать{' '}
              <span className="muted">— «алкоголь не употребляет» это запись, а не упоминание</span>
            </label>
          </li>
        </ul>
      )}
    </>
  )
}

/**
 * Набранные слова в список.
 *
 * Разделителем служит и запятая, и перевод строки: составитель вставляет
 * сюда столбик из чужого документа так же часто, как набирает через
 * запятую, и требовать одного значило бы молча терять второе.
 */
function словами(raw: string): string[] {
  return raw
    .split(/[,\n;]/)
    .map((one) => one.trim())
    .filter((one) => one !== '')
}

/**
 * Набранный предел числом.
 *
 * Ненабранное и набранное не числом — ноль, а ноль на сервере значит
 * «предела нет». Своего отказа здесь нет намеренно: отказ по пределам
 * пишет сервер («не назван ни нижний предел, ни верхний»), и вторая его
 * редакция разошлась бы с первой молча.
 */
function числом(raw: string): number {
  const n = Number(raw.replace(/\s/g, ''))
  return Number.isFinite(n) && n > 0 ? Math.trunc(n) : 0
}

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
