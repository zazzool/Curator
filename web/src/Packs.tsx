import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { счётом } from './words'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { confirmed } from './confirm'
import type { Case, Me, PackItem } from './api'
import { Banner } from './components/Banner'

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

// Линейка — чем набор открывается, и это же граница бесплатного.
//
// Словами, а не отсутствием цены: набор, у которого цены просто нет,
// выглядит недооценённым, а набор с линейкой «спонсорский» — подаренным.
// Первое составитель читает как недоделку и идёт ставить цену.
const LINE: Record<string, string> = {
  guest: 'гостевой — открыт всем, и до входа',
  basic: 'базовый — открыт тому, кто привязал почту',
  paid: 'платный — по подписке или покупке',
  sponsored: 'спонсорский — открыт всем, за него заплатил спонсор',
}

// Короткое имя линейки — для метки в списке, где длинному пояснению не
// место.
const LINE_TAG: Record<string, string> = {
  guest: 'гостевой',
  basic: 'базовый',
  paid: 'платный',
  sponsored: 'спонсорский',
}

export function Packs({ me }: { me: Me }) {
  const [open, setOpen] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState({ slug: '', title: '', summaryMd: '', line: 'paid' })
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
      // Карточка открывается по метке, которую вернул СЕРВЕР, а не по
      // набранной. Сервер метку приводит к своему виду (обрезает,
      // опускает регистр), и открытая по набранной карточка не нашлась
      // бы: составитель завёл набор и тут же прочитал «такого набора
      // нет».
      const { slug } = await api.createPack(draft)
      setAdding(false)
      setDraft({ slug: '', title: '', summaryMd: '', line: 'paid' })
      await reload()
      setOpen(slug)
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
            <span className="fld-label">Кому открыт</span>
            <select
              className="fld-long"
              value={draft.line}
              onChange={(e) => setDraft({ ...draft, line: e.target.value })}
            >
              {Object.entries(LINE).map(([value, name]) => (
                <option key={value} value={value}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label className="form-row">
            <span className="fld-label">Что внутри</span>
            <textarea
              value={draft.summaryMd}
              onChange={(e) => setDraft({ ...draft, summaryMd: e.target.value })}
            />
          </label>
          <p className="hint">
            Платный стоит первым умолчанием намеренно: набор, закрытый по
            ошибке, виден сразу — врач его не получит и скажет; набор,
            открытый по ошибке, не виден никому, и узнают о нём по
            непришедшим деньгам.
          </p>
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

      {failure && <Banner kind="error">{failure}</Banner>}

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
                  {/* Кому открыт — рядом с состоянием: на список смотрят,
                      чтобы увидеть, что и кому раздаётся, и уходить за
                      этим в карточку каждого набора незачем. */}
                  <span className="tag">{LINE_TAG[pack.line] ?? pack.line}</span>
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
  const [card, setCard] = useState({
    title: '', summaryMd: '', status: 'published', line: 'paid',
  })
  const [failure, setFailure] = useState('')
  // Опоздавшая правка — отдельное состояние, а не просто отказ: у неё
  // единственный выход, и его надо дать рядом со словами. Сам по себе
  // отказ оставляет составителя с набранным, которое больше никогда не
  // сохранится, и без подсказки, что делать.
  const [overtaken, setOvertaken] = useState(false)
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
    setCard({
      title: pack.title,
      summaryMd: pack.summaryMd,
      status: pack.status,
      line: pack.line,
    })
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
    if (pack === null) return
    setFailure('')
    setOvertaken(false)
    setNote('')
    setBusy(true)
    try {
      await api.setPackItems(slug, items.map((one) => one.id), pack.revision)
      await reload()
      setNote('Состав сохранён. На устройства он попадёт следующим выпуском.')
    } catch (error) {
      failed(error, 'Состав не сохранён')
    } finally {
      setBusy(false)
    }
  }

  // Перечитывание при отказе НЕ делается само: набранный состав держится
  // на экране, и перечитывание стёрло бы его — двадцать минут перестановок
  // вместе с ними. Решает составитель, и решает, уже увидев отказ.
  function failed(error: unknown, ifUnknown: string) {
    if (error instanceof ApiError) {
      setFailure(error.message)
      setOvertaken(error.status === 409)
      return
    }
    setFailure(ifUnknown)
  }

  async function saveCard() {
    if (pack === null) return
    setFailure('')
    setOvertaken(false)
    setNote('')
    // Снятие с витрины спрашивается, остальная правка карточки — нет:
    // название и описание исправляются тем же полем, а снятый набор
    // пропадает у всех врачей, и произойдёт это внутри «сохранить».
    if (
      card.status === 'retired' &&
      pack !== null &&
      pack.status !== 'retired' &&
      !confirmed(
        `Снять набор «${card.title}» с витрины?`,
        'В приложении его больше не предложат. Уже скачанное у врачей остаётся: ' +
          'подписанный выпуск не отзывается.',
      )
    ) {
      return
    }
    setBusy(true)
    try {
      await api.savePack(slug, { ...card, revision: pack.revision })
      await reload()
      setNote('Карточка сохранена.')
    } catch (error) {
      failed(error, 'Карточка не сохранена')
    } finally {
      setBusy(false)
    }
  }

  async function release() {
    setFailure('')
    setNote('')
    // Единственное действие студии, которое уезжает НАРУЖУ и подписью:
    // выпуск уходит на телефоны врачей, и отозвать подписанное нельзя —
    // так и написано на самом экране. Спрашивается поэтому всегда, а не
    // только в одну сторону.
    if (
      !confirmed(
        `Выпустить набор «${card.title}» составом из ` +
          `${счётом(items.length, 'задачи', 'задач', 'задач')}?`,
        'Выпуск подписывается и уходит на устройства врачей. Отозвать подписанное нельзя: ' +
          'исправляется оно только следующим выпуском.',
      )
    ) {
      return
    }
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

      {failure && (
        <Banner kind="error">
          {failure}
          {overtaken && (
            <>
              {' '}
              <button
                type="button"
                className="link-button"
                onClick={() => {
                  setFailure('')
                  setOvertaken(false)
                  void reload()
                }}
              >
                Перечитать набор
              </button>
            </>
          )}
        </Banner>
      )}
      {note && <Banner kind="success">{note}</Banner>}

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
              <span className="fld-label">Кому открыт</span>
              <select
                className="fld-long"
                value={card.line}
                onChange={(e) => setCard({ ...card, line: e.target.value })}
              >
                {Object.entries(LINE).map(([value, name]) => (
                  <option key={value} value={value}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <p className="hint">
              Линейка решает, кому набор открыт; цена — почём он продаётся.
              Выключенная цена платный набор не открывает: снятое с продажи
              не то же самое, что подаренное.
            </p>
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
  const read = useCallback(async () => {
    const answer = await api.cases({ status: 'published', limit: 200 })
    return { all: answer?.cases ?? [], more: answer?.more === true }
  }, [])
  const found = useResource(read, 'Задачи не прочитаны')

  return (
    <div className="page-section">
      <Loaded from={found} while="Читаем задачи…">
      {({ all, more }) => {
      const free = all.filter((one) => !chosen.includes(one.id))
      // Три разных положения, и раньше все три говорили одно.
      // «Не нашлось» при двухстах задачах, все из которых уже в наборе, —
      // утверждение ложное: их могут быть тысячи, просто подбор берёт
      // двести за раз. Составитель верил ему и считал набор собранным.
      return free.length === 0 ? (
        <p className="empty">
          {all.length === 0
            ? 'Раздаваемых задач не нашлось. Набор собирается из опубликованных: черновик не доедет ни до ленты, ни до повторения.'
            : more
              ? 'Все показанные задачи уже в наборе, а раздаваемых больше, чем подбор берёт за раз. Выпустите этот набор и соберите следующий: отбора по источнику у подбора пока нет.'
              : 'Все раздаваемые задачи уже в наборе.'}
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

