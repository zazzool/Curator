import { useCallback, useEffect, useState, type ChangeEvent } from 'react'

import { Cases } from './Cases'
import { Generation } from './Generation'
import { ApiError, api } from './api'
import type { Document, Me, Source, Statement, Unit } from './api'

// Экран источника: путь первого этапа целиком и в том же порядке, в каком
// его проходят, — принести документ, посмотреть куски, принять разбор,
// увидеть принятое.
//
// Разделы не прячутся от человека без права: право проверяет сервер, а
// скрытая вкладка при открытой ручке — подсказка, где искать, а не запрет.
// Здесь скрывается только действие, которое всё равно отказало бы, и рядом
// сказано, почему его нет.
export function SourceScreen({ me, id, onBack }: { me: Me; id: number; onBack: () => void }) {
  const [source, setSource] = useState<Source | null>(null)
  const [units, setUnits] = useState<Unit[]>([])
  const [documents, setDocuments] = useState<Document[]>([])
  const [path, setPath] = useState('')
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  /** Разбор какого документа открыт на просмотр. */
  const [looking, setLooking] = useState<Document | null>(null)

  const canAccept = me.permissions.includes('source:accept')

  const reload = useCallback(
    async (slicePath: string) => {
      setFailure('')
      try {
        const [loaded, sliced, docs] = await Promise.all([
          api.source(id),
          api.units(id, slicePath),
          api.documents(id),
        ])
        setSource(loaded)
        setUnits(sliced.units)
        setDocuments(docs.documents)
      } catch (error) {
        setFailure(error instanceof ApiError ? error.message : 'Источник не прочитан')
      }
    },
    [id],
  )

  useEffect(() => {
    void reload(path)
  }, [reload, path])

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
      await reload(path)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Документ не принят')
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
      await reload(path)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Разбор не принят')
    } finally {
      setBusy(false)
    }
  }

  async function setStatus(status: string) {
    setBusy(true)
    setFailure('')
    setNote('')
    try {
      const updated = await api.setSourceStatus(id, status)
      setSource(updated)
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
          <button onClick={onBack}>К источникам</button>
        </div>
        {failure ? <p className="banner error">{failure}</p> : <p className="empty">Читаем источник…</p>}
      </div>
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>{source.title}</h2>
        <button onClick={onBack}>К источникам</button>
      </div>
      <p className="hint">
        {source.slug} · {source.completeness === 'complete' ? 'полный справочник' : 'разобранный кусок'} ·
        единица зовётся «{source.unitWord}», положение — «{source.statementWord}»
      </p>

      {failure && <p className="banner error">{failure}</p>}
      {note && <p className="banner success">{note}</p>}

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
          {canAccept && (
            <label>
              <input type="file" onChange={upload} disabled={busy} />
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
                {canAccept && (
                  // Смотреть, а не принимать. Принятый разбор — это
                  // решение о том, что теперь считается истиной
                  // источника, и принималось оно вслепую: ни кусков, ни
                  // разобранных единиц составителю не показывалось.
                  // Пояснение к этому файлу обещало «посмотреть куски» —
                  // и обещало это годом раньше, чем появилось.
                  <button
                    onClick={() =>
                      setLooking(looking?.id === document.id ? null : document)
                    }
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

        {looking && (
          <DraftView
            document={looking}
            busy={busy}
            onAccept={() => void accept(looking.id)}
          />
        )}
      </section>

      <section className="page-section">
        <div className="page-head">
          <h2>Принятые {source.unitWord === '' ? 'единицы' : `${source.unitWord}ы`}</h2>
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
      </section>

      <Generation me={me} source={source} units={units} />

      <Cases me={me} source={source} path={path} />
    </div>
  )
}

/**
 * Что даст принятие разбора: единицы и положения, вычитанные из документа.
 *
 * Показывается ДО принятия и только перед ним. Принятый разбор меняет то,
 * что источник считает истиной, и по нему потом отбираются задачи; принять
 * его, не посмотрев, значит согласиться с чужим чтением документа не
 * читая. Ручки для показа были написаны и не звались ниоткуда.
 *
 * Принятие живёт здесь же, а не на строке документа, и это не дробность:
 * кнопка, стоящая рядом с тем, что она примет, не даёт принять вслепую.
 */
function DraftView({
  document,
  busy,
  onAccept,
}: {
  document: Document
  busy: boolean
  onAccept: () => void
}) {
  const [units, setUnits] = useState<Unit[] | null>(null)
  const [statements, setStatements] = useState<Statement[]>([])
  const [failure, setFailure] = useState('')

  useEffect(() => {
    let живы = true
    void (async () => {
      setFailure('')
      setUnits(null)
      try {
        const loaded = await api.draft(document.id)
        if (!живы) return
        // Список без списка — пустой список, а не падение раздела.
        setUnits(loaded?.units ?? [])
        setStatements(loaded?.statements ?? [])
      } catch (error) {
        if (!живы) return
        setUnits([])
        setFailure(error instanceof ApiError ? error.message : 'Разбор не прочитан')
      }
    })()
    return () => {
      живы = false
    }
  }, [document.id])

  const положенийУ = (label: string) =>
    statements.filter((one) => one.unitLabel === label).length

  return (
    <div className="page-section">
      <div className="page-head">
        <h3>Разбор файла «{document.filename}»</h3>
      </div>
      {failure && <p className="banner error">{failure}</p>}
      {units === null ? (
        <p className="empty">Читаем…</p>
      ) : units.length === 0 ? (
        <p className="empty">
          Из этого файла не вычиталось ни одной единицы. Принимать нечего:
          проверьте, тот ли это файл и тем ли способом он переведён в текст.
        </p>
      ) : (
        <>
          <p className="hint">
            Вычитано единиц: {units.length}, положений: {statements.length}.
            Принятое станет истиной источника, и по нему пойдёт подбор задач.
          </p>
          <ul className="units">
            {units.map((unit) => (
              <li key={unit.label} style={{ paddingLeft: `${unit.depth * 16}px` }}>
                <span className="mono">{unit.label}</span> {unit.title}
                {unit.kind === 'group' && <span className="tag">раздел</span>}
                {/* Единица без положений видна сразу: по ней нельзя
                    заказать задачу, и узнать об этом лучше здесь, чем в
                    генерации отказом. */}
                {unit.kind !== 'group' && положенийУ(unit.label) === 0 && (
                  <span className="muted"> — положений нет</span>
                )}
              </li>
            ))}
          </ul>
          <div className="form-actions">
            <button className="primary" onClick={onAccept} disabled={busy}>
              Принять разбор
            </button>
          </div>
        </>
      )}
    </div>
  )
}
