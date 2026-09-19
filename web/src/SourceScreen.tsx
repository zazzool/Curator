import { useCallback, useEffect, useState, type ChangeEvent } from 'react'

import { Cases } from './Cases'
import { Generation } from './Generation'
import { ApiError, api } from './api'
import type { Document, Me, Source, Unit } from './api'

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
        {failure ? <p className="alarm">{failure}</p> : <p className="empty">Читаем источник…</p>}
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

      {failure && <p className="alarm">{failure}</p>}
      {note && <p className="done">{note}</p>}

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
                  <button onClick={() => accept(document.id)} disabled={busy}>
                    Принять разбор
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
      </section>

      <section className="page-section">
        <div className="page-head">
          <h2>Принятые {source.unitWord === '' ? 'единицы' : `${source.unitWord}ы`}</h2>
          <label className="form-row">
            <span className="fld-label">Срез по пути</span>
            <input
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
                <span className="label">{unit.label}</span> {unit.title}
                {/* Род показывается только у раздела: по нему не спрашивают, и
                    составитель, не видя этого, ищет пропавшие задачи в
                    генерации, а не в разборе. У записи род — умолчание, и
                    метка у каждой строки была бы шумом. */}
                {unit.kind === 'group' && <span className="kind">раздел</span>}
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
