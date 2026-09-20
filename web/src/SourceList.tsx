import { useCallback, useState, type FormEvent } from 'react'

import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import type { Me } from './api'

// Словари закрыты на сервере, и здесь они повторены списками выбора: поле,
// куда можно вписать что угодно, отдало бы серверу значение, которое тот
// отклонит, — и человек узнал бы об этом после заполнения всей формы.
const KINDS: [string, string][] = [
  ['classification', 'классификация'],
  ['decree', 'приказ'],
  ['guidelines', 'клинические рекомендации'],
  ['standard', 'стандарт'],
  ['handbook', 'руководство'],
  ['other', 'другое'],
]

const PURPOSES: [string, string][] = [
  ['topic', 'по темам'],
  ['system', 'по системам органов'],
  ['discipline', 'по дисциплинам'],
  ['task', 'по видам задач'],
  ['level', 'по уровню подготовки'],
  ['legal', 'по правовым нормам'],
  ['other', 'по-другому'],
]

const HIERARCHIES: [string, string][] = [
  ['part-of', 'вложенное — часть целого'],
  ['is-a', 'вложенное — разновидность'],
  ['grouped', 'вложенное просто сгруппировано'],
]

const COMPLETENESS: [string, string][] = [
  ['complete', 'полный справочник'],
  ['fragment', 'разобранный кусок'],
]

const EMPTY = {
  slug: '',
  kind: 'decree',
  title: '',
  unitWord: '',
  statementWord: '',
  purpose: 'legal',
  hierarchy: 'part-of',
  completeness: 'fragment',
  edition: '',
}

export function SourceList({
  me,
  onOpen,
}: {
  me: Me
  // Название уходит наверх вместе с опознавателем: его ждёт верхняя
  // полоса, а экран источника читает его сам и отвечает позже, чем полоса
  // рисуется.
  onOpen: (id: number, title: string) => void
}) {
  const [failure, setFailure] = useState('')
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState(EMPTY)

  const canAccept = me.permissions.includes('source:accept')

  // Отказ чтения живёт в самом чтении, а не в общем `failure`: смешай их —
  // и отказ заведения источника гасился бы удачным перечитыванием списка,
  // которое идёт сразу за ним.
  const read = useCallback(async () => (await api.sources()).sources, [])
  const sources = useResource(read, 'Список источников не прочитан')
  const reload = sources.reload

  async function create(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    try {
      const { id } = await api.createSource(draft)
      setAdding(false)
      setDraft(EMPTY)
      await reload()
      onOpen(id, draft.title)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Источник не заведён')
    }
  }

  return (
    <div>
      <div className="page-head">
        <h2>Источники</h2>
        {canAccept && (
          // Синим отмечено то, ради чего открыт раздел. Отказ от заведения
          // синим не отмечается: две синие кнопки подряд перестают что-либо
          // выделять.
          <button className={adding ? undefined : 'primary'} onClick={() => setAdding(!adding)}>
            {adding ? 'Не заводить' : 'Завести источник'}
          </button>
        )}
      </div>
      <p className="hint">
        Источник — то, по чему пишутся задачи и чем они поверяются:
        классификация, приказ, рекомендации, стандарт, руководство.
      </p>

      {adding && (
        <form className="page-section form-grid" onSubmit={create}>
          <label className="form-row">
            <span className="fld-label">Краткое имя</span>
            <input
              className="fld-medium"
              value={draft.slug}
              onChange={(e) => setDraft({ ...draft, slug: e.target.value })}
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Название</span>
            <input
              className="fld-long"
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Вид</span>
            <select
              className="fld-medium"
              value={draft.kind}
              onChange={(e) => setDraft({ ...draft, kind: e.target.value })}
            >
              {KINDS.map(([value, name]) => (
                <option key={value} value={value}>
                  {name}
                </option>
              ))}
            </select>
          </label>
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
          <label className="form-row">
            <span className="fld-label">По чему делит материал</span>
            <select
              className="fld-long"
              value={draft.purpose}
              onChange={(e) => setDraft({ ...draft, purpose: e.target.value })}
            >
              {PURPOSES.map(([value, name]) => (
                <option key={value} value={value}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label className="form-row">
            <span className="fld-label">Что значит вложенность</span>
            <select
              className="fld-long"
              value={draft.hierarchy}
              onChange={(e) => setDraft({ ...draft, hierarchy: e.target.value })}
            >
              {HIERARCHIES.map(([value, name]) => (
                <option key={value} value={value}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label className="form-row">
            <span className="fld-label">Полнота</span>
            <select
              className="fld-medium"
              value={draft.completeness}
              onChange={(e) => setDraft({ ...draft, completeness: e.target.value })}
            >
              {COMPLETENESS.map(([value, name]) => (
                <option key={value} value={value}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <p className="hint">
            Полнота честна: разобранный из файла кусок — это кусок. На полноту
            опирается расчёт охвата, и объявленный полным кусок даёт ложные
            доли.
          </p>
          <div className="form-actions">
            <button className="primary" type="submit">
              Завести
            </button>
          </div>
        </form>
      )}

      {failure && <p className="banner error">{failure}</p>}

      <div className="page-section">
        <Loaded from={sources} while="Читаем список…">
        {(list) => list.length === 0 ? (
          // Пустая страница даёт выход с себя самой, как у донора: тот, кто
          // пришёл на пустой раздел, пришёл его наполнять, и отправлять его
          // глазами обратно к заголовку незачем. Когда форма уже открыта,
          // второй такой кнопки не показывается.
          <div className="empty stack">
            <p>Источников пока нет. Заведите первый — и принесите в него документ.</p>
            {canAccept && !adding && (
              <div className="toolbar" style={{ justifyContent: 'center' }}>
                <button className="primary" onClick={() => setAdding(true)}>
                  Завести источник
                </button>
              </div>
            )}
          </div>
        ) : (
          <div className="list">
            {list.map((source) => (
              <button
                key={source.id}
                className="list-row"
                onClick={() => onOpen(source.id, source.title)}
              >
                <span>{source.title}</span>
                <span className="muted">
                  {source.slug} · {source.completeness === 'complete' ? 'полный' : 'кусок'}
                </span>
              </button>
            ))}
          </div>
        )}
        </Loaded>
      </div>
    </div>
  )
}
