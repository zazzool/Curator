import { useCallback, useEffect, useState, type ChangeEvent } from 'react'

import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import { go } from './router'
import { словарём } from './sourceWords'
import { счётом } from './words'
import type { Document, Job, Me, Statement, Unit } from './api'
import { Banner } from './components/Banner'

/**
 * Ввоз документа в источник: принести файл, отдать его модели, выверить
 * разбор и принять.
 *
 * Своя страница, а не раздел карточки источника, потому что это работа
 * на полчаса с чужим документом перед глазами: приказ на сотню страниц
 * разбирается минутами, разбор потом вычитывается строка за строкой, и
 * делить экран с паспортом ей незачем. Адрес у страницы свой — ввоз
 * бросают на середине и возвращаются к нему завтра, и «открой источник,
 * пролистай до документов» вместо ссылки здесь стоило бы дороже всего.
 *
 * Разделы не прячутся от человека без права: право проверяет сервер, а
 * скрытая вкладка при открытой ручке — подсказка, где искать, а не
 * запрет. Здесь скрывается только действие, которое всё равно отказало
 * бы, и рядом сказано, почему его нет.
 */
export function SourceImport({
  me,
  id,
  onTitle,
}: {
  me: Me
  id: number
  /** Прочитанное название — верхней полосе: в адресе стоит только номер. */
  onTitle: (title: string) => void
}) {
  const [documents, setDocuments] = useState<Document[]>([])
  const [jobs, setJobs] = useState<Job[]>([])
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  /** Разбор какого документа открыт на просмотр. */
  const [looking, setLooking] = useState<Document | null>(null)

  const canAccept = me.permissions.includes('source:accept')
  // Разбор закрыт правом генерации, а не правом принимать: он тратит
  // деньги у поставщика моделей и кладёт черновик, а истину источника не
  // меняет. Кнопка прячется только потому, что всё равно отказала бы, и
  // рядом сказано почему.
  const canParse = me.permissions.includes('generate')

  const read = useCallback(async () => {
    const [loaded, docs] = await Promise.all([api.source(id), api.documents(id)])
    // Список без списка — пустой список, а не падение экрана: перебор
    // отсутствующего бросает во время отрисовки, а отказ отрисовки
    // снимает дерево целиком.
    setDocuments(docs.documents ?? [])
    return loaded
  }, [id])
  const opened = useResource(read, 'Источник не прочитан')
  const source = opened.state === 'ready' ? opened.value : null

  const title = source?.title ?? ''
  useEffect(() => {
    // Название уходит наверх после чтения, а не во время отрисовки:
    // правка чужого состояния прямо в теле отрисовки — это второй проход
    // по всему дереву на каждый кадр.
    if (title !== '') onTitle(title)
  }, [title, onTitle])

  /**
   * Список разборов в работе, и опрос — только пока в нём есть чего ждать.
   *
   * Разбор идёт минутами, и без опроса составитель смотрел бы на
   * «поставлено в очередь» до перезагрузки страницы. Но опрос «на всякий
   * случай» на открытой сутками вкладке — это тысячи обращений в никуда,
   * и замечают их по счёту за трафик; поэтому признак считается по
   * ПРОЧИТАННОМУ, а не по тому, заказывали ли мы разбор.
   */
  const [ticks, setTicks] = useState(0)
  useEffect(() => {
    let живо = true
    void (async () => {
      try {
        const answer = await api.jobs(id, 20)
        if (!живо) return
        setJobs((answer.jobs ?? []).filter((one) => one.kind === 'parse'))
      } catch {
        // Отказ чтения очереди гасит сам опрос: на истёкшей сессии
        // каждое обращение отвечает отказом, и биться в неё раз в три
        // секунды бессмысленно. Показанное при этом остаётся — пропажа
        // списка выглядела бы как «разборов не было».
        if (живо) setJobs([])
      }
    })()
    return () => {
      живо = false
    }
  }, [id, ticks])

  const ждём = jobs.some((one) => one.status === 'queued' || one.status === 'running')
  useEffect(() => {
    if (!ждём) return
    const timer = setInterval(() => setTicks((n) => n + 1), 3000)
    return () => clearInterval(timer)
  }, [ждём])

  async function upload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    // Поле очищается сразу: иначе тот же файл, выбранный второй раз, не
    // вызовет события, и человек решит, что студия его не слышит.
    event.target.value = ''
    if (!file) return
    setBusy(true)
    setFailure('')
    setNote('')
    try {
      const result = await api.upload(id, file)
      setNote(`Документ принят и разрезан на куски: ${result.fragments}. Формат — ${result.format}.`)
      await opened.reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Документ не принят')
    } finally {
      setBusy(false)
    }
  }

  // Разбор документа моделью: заказ уходит в очередь, а не выполняется
  // тут же. Документ на сотню страниц разбирается минутами, и держать на
  // нём вкладку нельзя — закрытая вкладка не должна отменять оплаченное.
  async function parse(documentId: number) {
    setBusy(true)
    setFailure('')
    setNote('')
    try {
      await api.parseDocument(documentId)
      setNote(
        'Документ отдан модели. Ход разбора виден ниже; разобранное ляжет ' +
          'черновиком, и принять его надо будет отдельно.',
      )
      setTicks((n) => n + 1)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Разбор не заказан')
    } finally {
      setBusy(false)
    }
  }

  async function accept(documentId: number) {
    setBusy(true)
    setFailure('')
    setNote('')
    try {
      const result = await api.accept(documentId)
      setNote(`Разбор принят. Единиц в источнике стало больше на ${result.accepted}.`)
      setLooking(null)
      await opened.reload()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Разбор не принят')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Loaded from={opened}>
      {(source) => (
        <div>
          <div className="page-head">
            <h2>Ввоз документов</h2>
            <button onClick={() => go({ name: 'source', id })}>К источнику</button>
          </div>
          <p className="hint">
            Источник «{source.title}». Принятое из документа станет его истиной:
            по {словарём(source.unitWord)} пойдёт подбор задач, а положения станут
            тем, что задача проверяет.
          </p>

          {failure && <Banner kind="error">{failure}</Banner>}
          {note && <Banner kind="success">{note}</Banner>}

          <section className="page-section">
            <div className="page-head">
              <h2>Документы</h2>
              {canAccept && (
                // Подпись у поля есть, хоть и не видна глазом: рамка выбора
                // файла рисуется браузером и словом «Обзор…» называет только
                // себя. Читающему с экрана без подписи достаётся «кнопка,
                // обзор» — и непонятно, что именно он приносит и куда.
                <label aria-label="Принести документ в источник">
                  {/*
                    accept перечисляет то же, что подсказка рядом, и теми же
                    расширениями, что разбирает сервер (source/upload.go).
                    Без него выбрать PDF можно, и узнаёт составитель об
                    отказе уже после загрузки — а файл у источника бывает на
                    десятки мегабайт.

                    Файл БЕЗ расширения сервер принимает за текст, а сюда
                    его не вписать: перечисляются расширения, и безымянному
                    соответствия нет. Это подсказка, а не запрет — браузер
                    даёт переключиться на «все файлы», — но случай назван,
                    чтобы следующий не счёл отсев поломкой.
                  */}
                  <input
                    type="file"
                    accept=".txt,.text,.md,.markdown,.docx"
                    onChange={upload}
                    disabled={busy}
                  />
                </label>
              )}
            </div>
            <p className="hint">
              Принимаются текст, Markdown и DOCX. PDF переводит в текст отдельная
              служба — принесите переведённый файл.
            </p>
            {documents.length === 0 ? (
              <p className="empty">Документов пока нет.</p>
            ) : (
              <div className="list">
                {documents.map((document) => (
                  <div key={document.id} className="list-row">
                    <span>{document.filename}</span>
                    <span className="muted">
                      {Math.max(1, Math.round(document.byteSize / 1024))} КБ
                      {document.uploadedBy && ` · принёс ${document.uploadedBy}`}
                    </span>
                    {canParse && (
                      <button onClick={() => void parse(document.id)} disabled={busy}>
                        Разобрать моделью
                      </button>
                    )}
                    {canAccept && (
                      // Смотреть, а не принимать. Принятый разбор — это
                      // решение о том, что теперь считается истиной
                      // источника, и принималось оно вслепую: ни кусков, ни
                      // разобранных единиц составителю не показывалось.
                      <button
                        onClick={() => setLooking(looking?.id === document.id ? null : document)}
                        disabled={busy}
                      >
                        {looking?.id === document.id ? 'Закрыть разбор' : 'Посмотреть разбор'}
                      </button>
                    )}
                  </div>
                ))}
              </div>
            )}
            {!canAccept && (
              <p className="hint">
                Принести документ и принять разбор может тот, кому выдано право
                принимать: принятый разбор — это решение о том, что теперь
                считается истиной источника.
              </p>
            )}
            {!canParse && (
              <p className="hint">
                Отдать документ модели может тот, кому выдано право запускать
                генерацию: разбор обращается к модели и стоит денег.
              </p>
            )}
          </section>

          {jobs.length > 0 && (
            <section className="page-section">
              <div className="page-head">
                <h2>Разбор</h2>
              </div>
              <div className="list">
                {jobs.map((job) => (
                  <ParseRow key={job.id} job={job} />
                ))}
              </div>
            </section>
          )}

          {looking && (
            <DraftView
              document={looking}
              canWrite={canAccept}
              busy={busy}
              onAccept={() => void accept(looking.id)}
            />
          )}
        </div>
      )}
    </Loaded>
  )
}

/**
 * Строка разбора в работе.
 *
 * Состояние показывается словами: «failed» составителю не говорит
 * ничего. Причина отказа стоит прямо в строке, а не за нажатием —
 * спрятанная причина не читается никем.
 *
 * Замечания о СДЕЛАННОЙ работе показываются отдельно от отказа: задание,
 * где одна часть из восьмидесяти не далась модели, — это сделанная
 * работа с потерей, а не провал, и прочитать их надо до приёмки.
 */
function ParseRow({ job }: { job: Job }) {
  const словом =
    job.status === 'queued'
      ? 'в очереди'
      : job.status === 'running'
        ? `идёт: ${job.stepWord}`
        : job.status === 'done'
          ? 'разобран'
          : `не вышло: ${job.error}`
  return (
    <div className="list-row">
      <span>{job.documentName || `документ № ${job.id}`}</span>
      <span className="muted">{словом}</span>
      {job.notes.length > 0 && (
        <span className="muted">
          {счётом(job.notes.length, 'замечание', 'замечания', 'замечаний')}:{' '}
          {job.notes.join('; ')}
        </span>
      )}
    </div>
  )
}

/** Единица черновика в правке: род и название правятся, метка — нет. */
type DraftUnit = { label: string; parentLabel: string; title: string; kind: string }
/** Положение черновика в правке. */
type DraftStatement = {
  unitLabel: string
  kind: string
  designation: string
  body: string
  placeRef: string
}

/**
 * Что даст принятие разбора: единицы и положения, вычитанные из документа.
 *
 * Показывается ДО принятия и только перед ним. Принятый разбор меняет то,
 * что источник считает истиной, и по нему потом отбираются задачи; принять
 * его, не посмотрев, значит согласиться с чужим чтением документа не
 * читая.
 *
 * Правка живёт здесь же. Модель читает документ хорошо, но не безупречно:
 * она склеивает два пункта в один, принимает заголовок таблицы за
 * положение, роняет окончание длинной строки. До этой правки выбор был
 * из двух — принять чужую ошибку истиной источника или выбросить весь
 * разбор и заказать его заново за те же деньги. Ручка правки на сервере
 * при этом была написана и не звалась ниоткуда.
 */
function DraftView({
  document,
  canWrite,
  busy,
  onAccept,
}: {
  document: Document
  canWrite: boolean
  busy: boolean
  onAccept: () => void
}) {
  // Ответ обогнавшего чтения отсекает счётчик походов внутри чтения.
  const read = useCallback(async () => {
    const loaded = await api.draft(document.id)
    // Список без списка — пустой список, а не падение раздела.
    return {
      units: (loaded?.units ?? []) as Unit[],
      statements: (loaded?.statements ?? []) as Statement[],
    }
  }, [document.id])
  const draft = useResource(read, 'Разбор не прочитан')

  return (
    <div className="page-section">
      <div className="page-head">
        <h3>Разбор файла «{document.filename}»</h3>
      </div>
      <Loaded from={draft}>
        {({ units, statements }) =>
          units.length === 0 ? (
            <p className="empty">
              Из этого файла не вычиталось ни одной единицы. Принимать нечего:
              проверьте, тот ли это файл и тем ли способом он переведён в текст.
            </p>
          ) : (
            <DraftEditor
              key={`${document.id}:${units.length}:${statements.length}`}
              documentId={document.id}
              units={units.map((one) => ({
                label: one.label,
                parentLabel: one.parentLabel,
                title: one.title,
                kind: one.kind,
              }))}
              statements={statements.map((one) => ({
                unitLabel: one.unitLabel,
                kind: one.kind,
                designation: one.designation,
                body: one.body,
                placeRef: one.placeRef,
              }))}
              canWrite={canWrite}
              busy={busy}
              onSaved={() => void draft.reload()}
              onAccept={onAccept}
            />
          )
        }
      </Loaded>
    </div>
  )
}

/**
 * Правка разобранного перед приёмкой.
 *
 * Метка не правится, и это не недоделка. Метка — то, чем положение
 * держится за свою единицу и чем единица держится за родителя;
 * переименуй её здесь — и положения остались бы висеть на метке,
 * которой больше нет, молча. Ошибка в самой метке значит, что модель
 * прочитала документ не так, и лечится это повторным разбором, а не
 * подстановкой руками.
 *
 * Удаление единицы уносит её положения: положение без своей единицы
 * сервер всё равно не примет, а показать их осиротевшими значило бы
 * обещать приёмку, которая откажет.
 */
function DraftEditor({
  documentId,
  units: read,
  statements: readStatements,
  canWrite,
  busy,
  onSaved,
  onAccept,
}: {
  documentId: number
  units: DraftUnit[]
  statements: DraftStatement[]
  canWrite: boolean
  busy: boolean
  onSaved: () => void
  onAccept: () => void
}) {
  const [units, setUnits] = useState<DraftUnit[]>(read)
  const [statements, setStatements] = useState<DraftStatement[]>(readStatements)
  /**
   * Есть ли несохранённая правка.
   *
   * От неё зависит не подпись, а приёмка: правка живёт здесь, а принимает
   * сервер то, что лежит у него. Прими мы, не записав, — составитель
   * увидел бы свою правку на экране и принятым чужой разбор, и разошлись
   * бы они молча.
   */
  const [dirty, setDirty] = useState(false)
  const [failure, setFailure] = useState('')
  const [saving, setSaving] = useState(false)

  function правим(change: () => void) {
    change()
    setDirty(true)
    setFailure('')
  }

  function удалить(label: string) {
    const положений = statements.filter((one) => one.unitLabel === label).length
    if (
      !confirmed(
        `Выбросить «${label}» из разбора?`,
        положений === 0
          ? 'В источник эта единица не попадёт. Разбор при этом цел — выброшено только здесь, до приёмки.'
          : `Вместе с ней выбросится ${счётом(положений, 'положение', 'положения', 'положений')}: ` +
              'положение без своей единицы сервер не примет.',
      )
    ) {
      return
    }
    правим(() => {
      setUnits((was) => was.filter((one) => one.label !== label))
      setStatements((was) => was.filter((one) => one.unitLabel !== label))
    })
  }

  async function save() {
    setSaving(true)
    setFailure('')
    try {
      await api.saveDraft(documentId, { units, statements })
      setDirty(false)
      onSaved()
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Правка не записана')
    } finally {
      setSaving(false)
    }
  }

  const положенийУ = (label: string) => statements.filter((one) => one.unitLabel === label).length

  return (
    <>
      <p className="hint">
        Вычитано единиц: {units.length}, положений: {statements.length}.
        Принятое станет истиной источника, и по нему пойдёт подбор задач.
      </p>
      {failure && <Banner kind="error">{failure}</Banner>}
      <ul className="units">
        {units.map((unit) => (
          <li key={unit.label}>
            <span className="mono">{unit.label}</span>{' '}
            {canWrite ? (
              <input
                className="fld-wide"
                aria-label={`Название ${unit.label}`}
                value={unit.title}
                onChange={(e) =>
                  правим(() =>
                    setUnits((was) =>
                      was.map((one) =>
                        one.label === unit.label ? { ...one, title: e.target.value } : one,
                      ),
                    ),
                  )
                }
              />
            ) : (
              unit.title
            )}
            {canWrite ? (
              <label className="form-row">
                {/* Род правится списком, а не галочкой «раздел»: значений
                    у него два, но закрытый словарь сервера может стать
                    длиннее, а галочка это переживёт молча. */}
                <select
                  aria-label={`Род ${unit.label}`}
                  value={unit.kind === 'group' ? 'group' : 'entry'}
                  onChange={(e) =>
                    правим(() =>
                      setUnits((was) =>
                        was.map((one) =>
                          one.label === unit.label ? { ...one, kind: e.target.value } : one,
                        ),
                      ),
                    )
                  }
                >
                  <option value="entry">по ней спрашивают</option>
                  <option value="group">раздел</option>
                </select>
              </label>
            ) : (
              unit.kind === 'group' && <span className="tag">раздел</span>
            )}
            {/* Единица без положений видна сразу: по ней нельзя заказать
                задачу, и узнать об этом лучше здесь, чем в генерации
                отказом. */}
            {unit.kind !== 'group' && положенийУ(unit.label) === 0 && (
              <span className="muted"> — положений нет</span>
            )}
            {canWrite && (
              <button onClick={() => удалить(unit.label)} disabled={saving || busy}>
                Выбросить
              </button>
            )}
          </li>
        ))}
      </ul>

      {canWrite && (
        <div className="form-actions">
          <button onClick={() => void save()} disabled={!dirty || saving || busy}>
            Записать правку
          </button>
          {/* Приёмка закрыта, пока правка не записана: принимает сервер
              то, что лежит у него, а не то, что видно на экране. */}
          <button className="primary" onClick={onAccept} disabled={dirty || saving || busy}>
            Принять разбор
          </button>
        </div>
      )}
      {dirty && (
        <p className="hint">
          Правка пока только на экране. Запишите её — принимается то, что лежит
          на сервере, а не то, что видно здесь.
        </p>
      )}
    </>
  )
}
