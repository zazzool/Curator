import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import { go } from './router'
import { COMPLETENESS, HIERARCHIES, KINDS, PURPOSES, словарём } from './sourceWords'
import type { Cut, Me, Source, Unit } from './api'
import { счётом } from './words'
import { Banner } from './components/Banner'

/**
 * Карточка источника: паспорт, состояние и принятое.
 *
 * Экран источника был один и держал на себе всё — паспорт, документы,
 * разбор, принятые единицы, заказ задачи и список задач. Читалось это,
 * пока источник был один и пустой; на приказе в сотню пунктов до
 * документов надо было прокручивать мимо девятисот строк.
 *
 * Здесь осталось то, что отвечает на вопрос «что это за источник и в
 * каком он состоянии». Ввоз документа уехал на свою страницу: это работа
 * на полчаса с чужим документом перед глазами, и делить с ней экран
 * паспорту незачем.
 *
 * Разделы не прячутся от человека без права: право проверяет сервер, а
 * скрытая вкладка при открытой ручке — подсказка, где искать, а не
 * запрет. Здесь скрывается только действие, которое всё равно отказало
 * бы, и рядом сказано, почему его нет.
 */
export function SourceCard({
  me,
  id,
  onTitle,
}: {
  me: Me
  id: number
  /**
   * Прочитанное название — верхней полосе.
   *
   * В адресе стоит только номер, и пришедший по прямой ссылке иначе видел
   * бы полосу без названия до самого ухода с экрана: полоса рисуется
   * раньше, чем источник прочитан.
   */
  onTitle: (title: string) => void
}) {
  const [units, setUnits] = useState<Unit[]>([])
  /** Показаны ли все подошедшие единицы, и сколько их показывается за раз. */
  const [unitsCut, setUnitsCut] = useState<Cut>({})
  const [documents, setDocuments] = useState(0)
  const [path, setPath] = useState('')

  /**
   * Отбор, дошедший до сервера, отстаёт от набранного на треть секунды.
   *
   * Отбор висел прямо на `onChange`, и каждое нажатие клавиши слало по
   * запросу: «F31.2» — двадцать обращений за секунду, и все, кроме
   * последнего, заказаны за то, чего составитель уже не ищет.
   *
   * Ответы при этом друг друга не обгоняют: `useResource` считает походы
   * и отбрасывает ответ на отменённый. Задержка — про НЕ ПОСЫЛАТЬ, а не
   * про порядок ответов, и одно другого не заменяет.
   *
   * Треть секунды: пауза между нажатиями у печатающего человека короче,
   * а осознанная остановка длиннее.
   */
  const [applied, setApplied] = useState('')
  useEffect(() => {
    const timer = setTimeout(() => setApplied(path), 300)
    return () => clearTimeout(timer)
  }, [path])

  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)

  const canAccept = me.permissions.includes('source:accept')

  const read = useCallback(async () => {
    const [loaded, sliced, docs] = await Promise.all([
      api.source(id),
      api.units(id, applied),
      api.documents(id),
    ])
    // Список без списка — пустой список, а не падение экрана: ответ без
    // поля documents ронял ВЕСЬ экран источника белым, вместе с принятым,
    // к документам отношения не имеющим.
    setUnits(sliced.units ?? [])
    setUnitsCut({ limit: sliced.limit, more: sliced.more })
    setDocuments((docs.documents ?? []).length)
    return loaded
  }, [id, applied])
  const opened = useResource(read, 'Источник не прочитан')
  const source = opened.state === 'ready' ? opened.value : null

  // Название уходит наверх после чтения, а не во время отрисовки: правка
  // чужого состояния прямо в теле отрисовки — это второй проход по всему
  // дереву на каждый кадр, и React о нём предупреждает недаром.
  const title = source?.title ?? ''
  useEffect(() => {
    if (title !== '') onTitle(title)
  }, [title, onTitle])

  async function setStatus(status: string) {
    // Спрашивается только снятие: объявить источник действующим обратно
    // можно той же кнопкой, и подтверждение у обратимого приучает
    // отвечать «да» не читая.
    if (
      status === 'retired' &&
      !confirmed(
        `Снять источник «${source?.title ?? ''}» с раздачи?`,
        'В справочнике приложения его больше не будет — у всех врачей сразу. ' +
          'Задачи по нему останутся, но врач не увидит, откуда они.',
      )
    ) {
      return
    }
    setBusy(true)
    setFailure('')
    setNote('')
    try {
      const updated = await api.setSourceStatus(id, status)
      // Подменяем прочитанное, а не перечитываем: сервер уже вернул новое
      // состояние, и второй запрос показал бы составителю старое между
      // двумя ответами.
      opened.set(updated)
      setNote(
        updated.status === 'active'
          ? 'Источник объявлен действующим: врачи увидят его в справочнике приложения.'
          : 'Источник снят: в приложении его больше нет.',
      )
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Состояние не записано')
    } finally {
      setBusy(false)
    }
  }

  if (!source) {
    return (
      <div>
        <div className="page-head">
          <button onClick={() => go({ name: 'sources' })}>К источникам</button>
        </div>
        <Loaded from={opened} while="Читаем источник…">
          {() => null}
        </Loaded>
      </div>
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>{source.title}</h2>
        <button onClick={() => go({ name: 'sources' })}>К источникам</button>
      </div>

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      {editing ? (
        <PassportForm
          source={source}
          onSaved={(saved) => {
            opened.set(saved)
            setEditing(false)
            setNote('Паспорт источника записан.')
          }}
          onClose={() => setEditing(false)}
        />
      ) : (
        <Passport source={source} canEdit={canAccept} onEdit={() => setEditing(true)} />
      )}

      <section className="page-section">
        <div className="page-head">
          <h2>Состояние</h2>
          {canAccept &&
            (source.status === 'active' ? (
              <button onClick={() => void setStatus('retired')} disabled={busy}>
                Снять с раздачи
              </button>
            ) : (
              <button onClick={() => void setStatus('active')} disabled={busy}>
                Объявить действующим
              </button>
            ))}
        </div>
        <p className="hint">
          {source.status === 'active'
            ? 'Действующий: врачи видят его в справочнике приложения и скачивают на устройство.'
            : source.status === 'retired'
              ? 'Снят с раздачи: в приложении его нет. Принятый разбор при этом цел — снятие не стирает ничего.'
              : 'Черновик: для приложения этого источника не существует. Объявите действующим, когда разбор принят и выверен.'}
        </p>
      </section>

      <section className="page-section">
        <div className="page-head">
          <h2>Документы</h2>
          <button onClick={() => go({ name: 'import', id })}>
            {documents === 0 ? 'Принести документ' : 'Ввоз документов'}
          </button>
        </div>
        <p className="hint">
          {documents === 0
            ? 'В источник ещё ничего не принесено. Задачи пишутся по положениям, а положения берутся из документа.'
            : `Принесено ${счётом(documents, 'документ', 'документа', 'документов')}. Разбор и приёмка — на странице ввоза.`}
        </p>
      </section>

      <section className="page-section">
        <div className="page-head">
          <h2>Принятые {словарём(source.unitWord)}</h2>
          <label className="form-row">
            <span className="fld-label">Срез по пути</span>
            <input
              className="fld-medium"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="например, 3 или F3"
            />
          </label>
        </div>
        <p className="hint">
          Путь складывается из связи с родителем, а не из формата метки:
          «всё, что под 3» — это 3, 3.1, 3.2 и всё, что ниже.
        </p>
        {units.length === 0 ? (
          <p className="empty">
            {path
              ? 'Под этим путём ничего нет. Проверьте метку — она пишется так же, как в документе.'
              : 'В источник ещё ничего не принято.'}
          </p>
        ) : (
          <ul className="units">
            {units.map((unit) => (
              <li key={unit.label} style={{ paddingLeft: `${unit.depth * 16}px` }}>
                <span className="mono">{unit.label}</span> {unit.title}
                {/* Род показывается только у раздела: по нему не спрашивают, и
                    составитель, не видя этого, ищет пропавшие задачи в
                    генерации, а не в разборе. У записи род — умолчание, и
                    метка у каждой строки была бы шумом. */}
                {unit.kind === 'group' && <span className="tag">раздел</span>}
              </li>
            ))}
          </ul>
        )}
        {unitsCut.more && (
          // Сказано ПОД списком, а не полосой наверху: полоса наверху —
          // про действие целиком, а это про то, что видно прямо здесь.
          // И сказано числом: «показаны не все» без числа не даёт понять,
          // насколько не все.
          <p className="hint">
            Показаны первые {unitsCut.limit}. Подошло больше — сузьте срез по
            пути, чтобы увидеть остальные.
          </p>
        )}
      </section>

      {/* Задачи и заказ живут своими разделами, а не разделами этой
          страницы. Здесь остались двери туда, и обе уносят выбранный
          источник и срез с собой: метка, переписанная руками из одного
          списка в другой, ошибается. */}
      <section className="page-section">
        <div className="page-head">
          <h2>Задачи по этому источнику</h2>
        </div>
        <div className="toolbar">
          <button
            onClick={() => go({ name: 'cases', query: { source: id, path: applied || undefined } })}
          >
            Показать задачи
          </button>
          {me.permissions.includes('generate') && (
            <button className="primary" onClick={() => go({ name: 'generate', source: id })}>
              Создать задачу
            </button>
          )}
        </div>
      </section>
    </div>
  )
}

/**
 * Паспорт источника — то, что он объявил о себе.
 *
 * Объявление, которого не видно, остаётся тайной базы: по оси решается,
 * складываются ли два источника в один список, по полноте считаются доли
 * охвата, а словарь интерфейса — это слова, которыми студия и приложение
 * зовут единицу и положение. Всё это задавалось при заведении и дальше
 * не показывалось нигде.
 */
function Passport({
  source,
  canEdit,
  onEdit,
}: {
  source: Source
  canEdit: boolean
  onEdit: () => void
}) {
  return (
    <section className="page-section">
      <div className="page-head">
        <h2>Паспорт</h2>
        {canEdit && <button onClick={onEdit}>Править</button>}
      </div>
      <div className="table-wrap">
        <table className="table">
          <tbody>
            <tr>
              <th>Краткое имя</th>
              <td>
                <span className="mono">{source.slug}</span>
                {/* Сказано здесь, а не отказом при попытке: по этому
                    имени источник спрашивает приложение, и составитель
                    должен знать, что оно не правится, ДО того как
                    соберётся его править. */}
                <span className="muted"> — не правится: по нему источник спрашивает приложение</span>
              </td>
            </tr>
            <tr>
              <th>Вид</th>
              <td>{изсловаря(KINDS, source.kind)}</td>
            </tr>
            <tr>
              <th>Словарь</th>
              <td>
                единица зовётся «{source.unitWord}», положение — «{source.statementWord}»
              </td>
            </tr>
            <tr>
              <th>По чему делит материал</th>
              <td>{изсловаря(PURPOSES, source.purpose)}</td>
            </tr>
            <tr>
              <th>Что значит вложенность</th>
              <td>{изсловаря(HIERARCHIES, source.hierarchy)}</td>
            </tr>
            <tr>
              <th>Полнота</th>
              <td>{изсловаря(COMPLETENESS, source.completeness)}</td>
            </tr>
            <tr>
              <th>Редакция</th>
              <td>{source.edition || <span className="muted">не названа</span>}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  )
}

/**
 * Правка паспорта.
 *
 * Краткого имени здесь нет, и это не забывчивость: по нему приложение
 * спрашивает справочник, и сменённое оно означало бы для установленных
 * сборок, что источника больше нет. Поле, которое сервер примет и молча
 * отбросит, хуже отсутствующего.
 *
 * Всё остальное правится, потому что объявленное однажды по ошибке иначе
 * остаётся навсегда: полнота, названная неверно, врёт в долях охвата
 * ровно там, где на них смотрят, а словарь интерфейса показывает
 * «единицу» тому, кто ждёт слова «пункт».
 */
function PassportForm({
  source,
  onSaved,
  onClose,
}: {
  source: Source
  onSaved: (saved: Source) => void
  onClose: () => void
}) {
  const [draft, setDraft] = useState({
    kind: source.kind,
    title: source.title,
    unitWord: source.unitWord,
    statementWord: source.statementWord,
    purpose: source.purpose,
    hierarchy: source.hierarchy,
    completeness: source.completeness,
    edition: source.edition,
  })
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)

  async function save(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setBusy(true)
    try {
      onSaved(await api.updateSource(source.id, draft))
    } catch (error) {
      // Текст сервера как есть: он пишет его по-русски и говорит, чего не
      // хватает, а «проверьте поля» отправляет перебирать их вслепую.
      setFailure(error instanceof ApiError ? error.message : 'Паспорт не записан')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="page-section form-grid" onSubmit={save}>
      <div className="page-head">
        <h2>Правка паспорта</h2>
        <button type="button" onClick={onClose}>
          Отменить
        </button>
      </div>

      {failure && <Banner kind="error">{failure}</Banner>}

      <label className="form-row">
        <span className="fld-label">Название</span>
        <input
          className="fld-long"
          value={draft.title}
          onChange={(e) => setDraft({ ...draft, title: e.target.value })}
        />
      </label>
      <Picker
        label="Вид"
        className="fld-medium"
        words={KINDS}
        value={draft.kind}
        onPick={(kind) => setDraft({ ...draft, kind })}
      />
      <label className="form-row">
        <span className="fld-label">Как звать единицу</span>
        <input
          className="fld-medium"
          value={draft.unitWord}
          onChange={(e) => setDraft({ ...draft, unitWord: e.target.value })}
          placeholder="пункт, диагноз, раздел"
        />
      </label>
      <label className="form-row">
        <span className="fld-label">Как звать положение</span>
        <input
          className="fld-medium"
          value={draft.statementWord}
          onChange={(e) => setDraft({ ...draft, statementWord: e.target.value })}
          placeholder="положение, признак, требование"
        />
      </label>
      <Picker
        label="По чему делит материал"
        className="fld-long"
        words={PURPOSES}
        value={draft.purpose}
        onPick={(purpose) => setDraft({ ...draft, purpose })}
      />
      <Picker
        label="Что значит вложенность"
        className="fld-long"
        words={HIERARCHIES}
        value={draft.hierarchy}
        onPick={(hierarchy) => setDraft({ ...draft, hierarchy })}
      />
      <Picker
        label="Полнота"
        className="fld-medium"
        words={COMPLETENESS}
        value={draft.completeness}
        onPick={(completeness) => setDraft({ ...draft, completeness })}
      />
      <p className="hint">
        Полнота честна: разобранный из файла кусок — это кусок. На полноту
        опирается расчёт охвата, и объявленный полным кусок даёт ложные
        доли.
      </p>
      <label className="form-row">
        <span className="fld-label">Редакция</span>
        <input
          className="fld-medium"
          value={draft.edition}
          onChange={(e) => setDraft({ ...draft, edition: e.target.value })}
          placeholder="2026"
        />
      </label>

      <div className="form-actions">
        <button className="primary" type="submit" disabled={busy}>
          Записать паспорт
        </button>
      </div>
    </form>
  )
}

/**
 * Строка формы со списком выбора по закрытому словарю сервера.
 *
 * Подпись несёт сама, а не обёрнута в неё снаружи: подпись, накинутая на
 * вызов этой заготовки, связана с полем только на глаз — разбор разметки
 * видит `label` без своего поля и говорит об этом, а читающему с экрана
 * достаётся список без имени.
 */
function Picker({
  label,
  className,
  words,
  value,
  onPick,
}: {
  label: string
  className: string
  words: [string, string][]
  value: string
  onPick: (value: string) => void
}) {
  return (
    <label className="form-row">
      <span className="fld-label">{label}</span>
      <select
        className={className}
        aria-label={label}
        value={value}
        onChange={(e) => onPick(e.target.value)}
      >
        {words.map(([key, word]) => (
          <option key={key} value={key}>
            {word}
          </option>
        ))}
      </select>
    </label>
  )
}

/**
 * Значение словаря словами.
 *
 * Незнакомое показывается собой, а не подменяется первым из словаря:
 * источник, заведённый сервером новее студии, иначе выглядел бы
 * приказом, ничего об этом не сказав.
 */
function изсловаря(words: [string, string][], value: string): string {
  return words.find(([key]) => key === value)?.[1] ?? value
}
