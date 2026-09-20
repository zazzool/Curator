import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { счётом } from './words'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import type { Case, Me, PackItem } from './api'

// Наборы задач: что собрано, из чего и что уехало на устройства.
//
// Раздел заведён последним из обязательных, и это не порядок красоты:
// вкладка «Наборы» в приложении оставалась пуста у всех и всегда, потому
// что собрать набор было нечем. Сервер умел всё — заводить, задавать
// состав, подписывать выпуск, — а дверь к этому была только у того, кто
// пишет запросы руками.

const STATUS: Record<string, string> = {
  draft: 'черновик',
  published: 'на витрине',
  retired: 'снят',
}

export function Packs({ me }: { me: Me }) {
  const [open, setOpen] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState({ slug: '', title: '', summaryMd: '' })
  const [failure, setFailure] = useState('')

  const canPack = me.permissions.includes('packs')

  // Отказ чтения живёт в самом чтении, а не в общем `failure`: смешай их —
  // и отказ заведения набора гасился бы удачным перечитыванием списка,
  // которое идёт сразу за ним.
  const read = useCallback(async () => (await api.packs())?.packs ?? [], [])
  const packs = useResource(read, 'Наборы не прочитаны')
  const reload = packs.reload

  async function create(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    try {
      await api.createPack(draft)
      setAdding(false)
      setDraft({ slug: '', title: '', summaryMd: '' })
      await reload()
      setOpen(draft.slug)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Набор не заведён')
    }
  }

  if (open !== null) {
    return (
      <PackCard
        me={me}
        slug={open}
        onBack={() => {
          setOpen(null)
          void reload()
        }}
      />
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>Наборы</h2>
        {canPack && (
          // Синее — у того, ради чего открыт раздел; отказ от заведения
          // синим не отмечается (см. тот же довод в SourceList).
          <button className={adding ? undefined : 'primary'} onClick={() => setAdding(!adding)}>
            {adding ? 'Не заводить' : 'Завести набор'}
          </button>
        )}
      </div>
      <p className="hint">
        Набор — это то, что врач скачивает целиком и разбирает без сети.
        Состав правится свободно, а на устройства попадает только выпуск:
        подписанный снимок состава на миг подписи.
      </p>
      {!canPack && (
        <p className="hint">
          Заводить и выпускать наборы может тот, кому выдано право «наборы».
        </p>
      )}

      {adding && (
        <form className="page-section form-grid" onSubmit={create}>
          <label className="form-row">
            <span className="fld-label">Метка</span>
            <input
              className="fld-medium"
              value={draft.slug}
              onChange={(e) => setDraft({ ...draft, slug: e.target.value })}
              placeholder="cardio-basics"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Название</span>
            <input
              className="fld-long"
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
              placeholder="Кардиология: основы"
            />
          </label>
          <label className="form-row">
            <span className="fld-label">Что внутри</span>
            <textarea
              value={draft.summaryMd}
              onChange={(e) => setDraft({ ...draft, summaryMd: e.target.value })}
            />
          </label>
          <p className="hint">
            Описание читает врач на витрине, и написано оно должно быть для
            него: «сорок задач по острому коронарному синдрому», а не
            «выборка по I21–I22».
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
        <Loaded from={packs} while="Читаем список…">
        {(list) => list.length === 0 ? (
          // Выход с пустой страницы — там же, где пустота объявлена
          // (донорская повадка): пришедший на пустой раздел пришёл его
          // наполнять.
          <div className="empty stack">
            <p>
              Наборов пока нет. Пока нет ни одного выпущенного, вкладка «Наборы»
              в приложении пуста у всех.
            </p>
            {canPack && !adding && (
              <div className="toolbar" style={{ justifyContent: 'center' }}>
                <button className="primary" onClick={() => setAdding(true)}>
                  Завести набор
                </button>
              </div>
            )}
          </div>
        ) : (
          <div className="list">
            {list.map((pack) => (
              <button key={pack.slug} className="list-row" onClick={() => setOpen(pack.slug)}>
                <span>
                  {pack.title}
                  {/* Цвет метки — от состояния: слово и цвет говорят об одном,
                      и на бегу читается цвет. */}
                  <span className={`tag ${pack.status}`}>{STATUS[pack.status] ?? pack.status}</span>
                </span>
                <span className="muted">
                  {счётом(pack.cases, 'задача', 'задачи', 'задач')} ·{' '}
                  {pack.version > 0 ? `выпуск ${pack.version}` : 'не выпускался'}
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

// Карточка набора: состав, правка описания и выпуск.
function PackCard({ me, slug, onBack }: { me: Me; slug: string; onBack: () => void }) {
  const [items, setItems] = useState<PackItem[]>([])
  const [card, setCard] = useState({ title: '', summaryMd: '', status: 'published' })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [picking, setPicking] = useState(false)
  const [busy, setBusy] = useState(false)

  const canPack = me.permissions.includes('packs')

  const read = useCallback(() => api.pack(slug), [slug])
  const opened = useResource(read, 'Набор не прочитан')
  const reload = opened.reload
  const pack = opened.state === 'ready' ? opened.value : null

  // Набранное составителем заводится с прочитанного и переписывается
  // только НОВЫМ ответом сервера, а не каждой отрисовкой: иначе правка
  // описания откатывалась бы на любое перечитывание.
  useEffect(() => {
    if (pack === null) return
    setItems(pack.items)
    setCard({ title: pack.title, summaryMd: pack.summaryMd, status: pack.status })
  }, [pack])

  // Состав на экране и состав в базе расходятся сразу, как только
  // составитель что-то переставил, и сказать ему об этом надо прямо:
  // ушедший с несохранённым составом не теряет ничего, кроме своей работы,
  // и узнаёт об этом, только вернувшись.
  const changed =
    pack !== null &&
    (items.length !== pack.items.length ||
      items.some((one, i) => one.id !== pack.items[i]?.id))

  async function saveItems() {
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      await api.setPackItems(slug, items.map((one) => one.id))
      await reload()
      setNote('Состав сохранён. На устройства он попадёт следующим выпуском.')
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Состав не сохранён')
    } finally {
      setBusy(false)
    }
  }

  async function saveCard() {
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      await api.savePack(slug, card)
      await reload()
      setNote('Карточка сохранена.')
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Карточка не сохранена')
    } finally {
      setBusy(false)
    }
  }

  async function release() {
    setFailure('')
    setNote('')
    setBusy(true)
    try {
      const out = await api.releasePack(slug)
      await reload()
      setNote(
        `Выпуск ${out.version} подписан ключом ${out.keyId}: ` +
          `${счётом(out.cases, 'задача', 'задачи', 'задач')}. Устройства увидят его при обновлении.`,
      )
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Выпуск не собран')
    } finally {
      setBusy(false)
    }
  }

  function move(index: number, to: number) {
    if (to < 0 || to >= items.length) return
    const next = [...items]
    const taken = next[index]
    if (taken === undefined) return
    next.splice(index, 1)
    next.splice(to, 0, taken)
    setItems(next)
  }

  if (pack === null) {
    return (
      <div>
        <div className="page-head">
          <h2>Набор</h2>
          <button onClick={onBack}>К наборам</button>
        </div>
        <Loaded from={opened} while="Читаем набор…">
          {() => null}
        </Loaded>
      </div>
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>{pack.title}</h2>
        <button onClick={onBack}>К наборам</button>
      </div>
      <p className="hint">
        {pack.slug} ·{' '}
        {pack.version > 0
          ? `на устройствах выпуск ${pack.version}`
          : 'ни одного выпуска: на устройствах этого набора нет'}
      </p>

      {failure && <p className="banner error">{failure}</p>}
      {note && <p className="banner success">{note}</p>}

      <div className="page-section">
        <h3>Состав</h3>
        {items.length === 0 ? (
          <p className="empty">
            Состав пуст. Выпустить пустой набор нельзя — добавьте задачи.
          </p>
        ) : (
          <ol className="units">
            {items.map((one, index) => (
              <li key={one.id} className="row-line">
                <span>
                  <span className="mono">{one.unitLabel}</span> {one.title}
                  {one.status !== 'published' && <span className="tag">не раздаётся</span>}
                </span>
                {canPack && (
                  <span className="row-tools">
                    <button onClick={() => move(index, index - 1)} disabled={index === 0}>
                      ↑
                    </button>
                    <button
                      onClick={() => move(index, index + 1)}
                      disabled={index === items.length - 1}
                    >
                      ↓
                    </button>
                    <button onClick={() => setItems(items.filter((x) => x.id !== one.id))}>
                      Убрать
                    </button>
                  </span>
                )}
              </li>
            ))}
          </ol>
        )}

        {canPack && (
          <div className="form-actions page-section">
            <button onClick={() => setPicking(!picking)}>
              {picking ? 'Не добавлять' : 'Добавить задачи'}
            </button>
            <button onClick={saveItems} disabled={!changed || busy}>
              Сохранить состав
            </button>
            {changed && <span className="hint">Состав изменён и пока не сохранён.</span>}
          </div>
        )}

        {picking && (
          <Picker
            chosen={items.map((one) => one.id)}
            onAdd={(one) =>
              setItems([
                ...items,
                {
                  id: one.id,
                  ord: items.length,
                  title: one.body.title,
                  unitLabel: one.unitLabel,
                  status: one.status,
                },
              ])
            }
          />
        )}
      </div>

      {canPack && (
        <div className="page-section">
          <h3>Карточка</h3>
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
              <span className="fld-label">Что внутри</span>
              <textarea
                value={card.summaryMd}
                onChange={(e) => setCard({ ...card, summaryMd: e.target.value })}
              />
            </label>
            <label className="form-row">
              <span className="fld-label">Состояние</span>
              <select
                className="fld-medium"
                value={card.status}
                onChange={(e) => setCard({ ...card, status: e.target.value })}
              >
                {Object.entries(STATUS).map(([value, name]) => (
                  <option key={value} value={value}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <p className="hint">
              Снятый набор пропадает с витрины, но уже скачанное у врача
              остаётся: подписанный выпуск не отзывается.
            </p>
            <div className="form-actions">
              <button onClick={saveCard} disabled={busy}>
                Сохранить карточку
              </button>
            </div>
          </div>
        </div>
      )}

      {canPack && (
        <div className="page-section">
          <h3>Выпуск</h3>
          <p className="hint">
            Выпуск подписывается нашим ключом и неизменяем. Правка состава
            после него ничего не меняет на устройствах до следующего выпуска.
          </p>
          <div className="form-actions">
            <button className="primary" onClick={release} disabled={busy || items.length === 0}>
              Выпустить
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// Подбор задач в набор.
//
// Показываются только раздаваемые: набор из черновиков соберётся, а врач
// получит выпуск, половины которого нет ни в ленте, ни в повторении.
function Picker({ chosen, onAdd }: { chosen: string[]; onAdd: (one: Case) => void }) {
  const read = useCallback(
    async () => (await api.cases({ status: 'published', limit: 200 }))?.cases ?? [],
    [],
  )
  const found = useResource(read, 'Задачи не прочитаны')

  return (
    <div className="page-section">
      <Loaded from={found} while="Читаем задачи…">
      {(all) => {
      const free = all.filter((one) => !chosen.includes(one.id))
      return free.length === 0 ? (
        <p className="empty">
          Раздаваемых задач, которых ещё нет в наборе, не нашлось. Набор
          собирается из опубликованных: черновик не доедет ни до ленты, ни до
          повторения.
        </p>
      ) : (
        <div className="list">
          {free.map((one) => (
            <button key={one.id} className="list-row" onClick={() => onAdd(one)}>
              <span>
                <span className="mono">{one.unitLabel}</span> {one.body.title}
              </span>
              <span className="muted">добавить</span>
            </button>
          ))}
        </div>
      )
      }}
      </Loaded>
    </div>
  )
}

