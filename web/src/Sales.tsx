import { useCallback, useRef, useState, type FormEvent } from 'react'

import { рублями, вКопейки } from './Money'
import { датой, счётом } from './words'
import { ApiError, api } from './api'
import { Loaded, useResource } from './useResource'
import { askReason, confirmed } from './confirm'
import type { Client, Entitlement, Me, Pack, Payment } from './api'
import { Banner } from './components/Banner'

// Продажи: кому продано, за что и почём.
//
// Раздел стоит на двух правах, и это не дробность ради дробности. Цены и
// приход — право «продажи»; карточка врача, его платежи и права — право
// «клиенты». Смотреть, за что человек заплатил, и решать, сколько стоит
// подписка, — занятия разных людей.
//
// Приход оформляет оператор, а не эквайер: первый круг живёт на ручном
// приходе, и заготовка под эквайер включается только после сверки подписи
// уведомления. Настройка, включённая раньше подписи, открывает всякому
// желающему ручку, которая оформляет права.

const SOURCE: Record<string, string> = {
  operator: 'оператор',
  acquirer: 'эквайер',
  store: 'магазин',
}

const PAYMENT_STATUS: Record<string, string> = {
  created: 'заведён',
  paid: 'оплачен',
  refunded: 'возвращён',
}

const ORIGIN: Record<string, string> = {
  registered: 'при заведении',
  purchase: 'покупка',
  subscription: 'подписка',
  grant: 'выдано руками',
}

export function Sales({ me }: { me: Me }) {
  const [open, setOpen] = useState<number | null>(null)

  if (open !== null) {
    return <ClientCard me={me} id={open} onBack={() => setOpen(null)} />
  }
  return (
    <div className="stack">
      <Prices me={me} />
      <ClientSearch onOpen={setOpen} />
    </div>
  )
}

// Цены: что и почём стоит на витрине.
function Prices({ me }: { me: Me }) {
  const [packs, setPacks] = useState<Pack[]>([])
  const [draft, setDraft] = useState({ purpose: 'subscription:month', rubles: '', enabled: true })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  /** Отказ по полю суммы: стоит под полем, а не полосой наверху. */
  const [суммаНеТа, setСуммаНеТа] = useState('')

  const canSell = me.permissions.includes('sales')

  const read = useCallback(async () => {
    const list = (await api.prices())?.prices ?? []
    try {
      setPacks((await api.packs())?.packs ?? [])
    } catch {
      // Наборы нужны только для списка назначений. Без права на них
      // оператор всё равно назовёт подписку — раздел из-за этого молчать
      // не должен, и отказ по ним не отказ раздела.
      setPacks([])
    }
    return list
  }, [])
  const prices = useResource(read, 'Цены не прочитаны')
  const reload = prices.reload

  async function save(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    setСуммаНеТа('')
    const kopecks = вКопейки(draft.rubles)
    if (kopecks === null) {
      // Отказ по полю стоит ПОД полем, а не полосой наверху страницы:
      // «Сумма пишется рублями и копейками» над формой из трёх полей не
      // говорит, о котором из них речь, и составитель перебирает их
      // вслепую. Полоса наверху остаётся за отказами сервера — они про
      // действие целиком.
      setСуммаНеТа('Рублями и копейками: 1990 или 1990,00')
      return
    }
    try {
      await api.setPrice({ purpose: draft.purpose, kopecks, enabled: draft.enabled })
      await reload()
      setNote('Цена сохранена.')
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Цена не сохранена')
    }
  }

  return (
    <div>
      <div className="page-head">
        <h2>Цены</h2>
      </div>
      <p className="hint">
        Цены — в рублях. Расход на модели считается в долларах и с этими
        числами не смешивается: перевод делается при показе и остаётся
        видимым как перевод.
      </p>
      {!canSell && (
        <p className="hint">Цены меняет тот, кому выдано право «продажи».</p>
      )}

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      <div className="page-section">
        <Loaded from={prices} while="Читаем цены…">
        {(list) => list.length === 0 ? (
          <p className="empty">
            Цен нет ни одной: витрина ничего не предлагает, и купить нечего.
          </p>
        ) : (
          <div className="list">
            {list.map((price) => (
              <div key={price.purpose} className="list-row">
                <span>
                  {назначением(price.purpose)}
                  {!price.enabled && <span className="tag retired">не продаётся</span>}
                </span>
                <span className="muted">{рублями(price.kopecks)}</span>
              </div>
            ))}
          </div>
        )}
        </Loaded>
      </div>

      {canSell && (
        <form className="page-section form-grid" onSubmit={save}>
          <label className="form-row">
            <span className="fld-label">За что</span>
            <select
              className="fld-long"
              value={draft.purpose}
              onChange={(e) => setDraft({ ...draft, purpose: e.target.value })}
            >
              <option value="subscription:month">подписка на месяц</option>
              <option value="subscription:year">подписка на год</option>
              {packs.map((pack) => (
                <option key={pack.slug} value={`pack:${pack.slug}`}>
                  набор «{pack.title}»
                </option>
              ))}
            </select>
          </label>
          <label className="form-row">
            <span className="fld-label">Сколько, рублей</span>
            <input
              className="fld-short"
              value={draft.rubles}
              onChange={(e) => {
                setDraft({ ...draft, rubles: e.target.value })
                setСуммаНеТа('')
              }}
              placeholder="1990"
              aria-invalid={суммаНеТа !== ''}
              aria-describedby={суммаНеТа === '' ? undefined : 'цена-сумма-отказ'}
            />
            {суммаНеТа !== '' && (
              <span id="цена-сумма-отказ" className="fld-error fld-across" role="alert">
                {суммаНеТа}
              </span>
            )}
          </label>
          <label className="form-row">
            <span className="fld-label">Продаётся</span>
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
            />
          </label>
          <div className="form-actions">
            {/* Сохранение открытой формы синим не отмечается: синее —
                у того, что заводит или двигает вперёд. */}
            <button type="submit">Сохранить цену</button>
          </div>
        </form>
      )}
    </div>
  )
}

// Поиск клиента.
//
// Заведён потому, что приход оформляется на номер учётной записи, а взять
// этот номер было негде: оператор, которому врач написал с почты, не мог
// найти его вовсе.
function ClientSearch({ onOpen }: { onOpen: (id: number) => void }) {
  const [query, setQuery] = useState('')
  // Ищется по ПОСЛЕДНЕМУ набранному, а не по тому, что было при нажатии:
  // запросы уходят по очереди, ответы возвращаются как придётся, и
  // счётчик походов внутри чтения не даёт обогнавшему затереть свежий.
  const [asked, setAsked] = useState('')
  const read = useCallback(async () => {
    const answer = await api.clients(asked)
    return { list: answer?.clients ?? [], more: answer?.more === true, limit: answer?.limit }
  }, [asked])
  const clients = useResource(read, 'Клиенты не прочитаны')

  return (
    <div className="page-section">
      <div className="page-head">
        <h2>Клиенты</h2>
      </div>
      {/* Поле и кнопка в одну строку: кнопка под полем читается как
          отдельное действие ни над чем. Мера поля — по ожидаемому ответу,
          а не во всю страницу: в него пишут имя или номер. */}
      <form
        className="field-with-action fld-long"
        onSubmit={(event) => {
          event.preventDefault()
          setAsked(query)
        }}
      >
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="почта, имя или номер"
          aria-label="Кого искать"
        />
        <button type="submit">Найти</button>
      </form>

      <div className="page-section">
        <Loaded from={clients}>
        {({ list, more, limit }) => list.length === 0 ? (
          <p className="empty">
            {query
              ? 'По этому запросу никого. Проверьте почту — искать можно и по части её.'
              : 'Клиентов пока нет: учётная запись заводится при первом запуске приложения.'}
          </p>
        ) : (
          <div className="list">
            {list.map((one) => (
              <button key={one.id} className="list-row" onClick={() => onOpen(one.id)}>
                <span>
                  {именем(one)}
                  {one.blocked && <span className="tag">вход закрыт</span>}
                </span>
                <span className="muted">
                  {/* Номер здесь не повторяется: у врача без имени и почты
                      он уже стоит слева, и строка «№ 6 · № 6» читается как
                      сбой, а не как сведения. */}
                  {one.displayName || one.email ? `№ ${one.id} · ` : ''}
                  {one.email && one.displayName ? `${one.email} · ` : ''}
                  {one.rights > 0 ? `прав: ${one.rights}` : 'прав нет'}
                </span>
              </button>
            ))}
            {more && (
              // Оператор, увидевший полный список без пятьдесят первого
              // врача, заводит вторую учётную запись тому, у кого она
              // есть. Числом, а не словами «показаны не все»: без числа
              // непонятно, насколько не все.
              <p className="hint">
                Показаны первые {limit}. Подошло больше — уточните запрос.
              </p>
            )}
          </div>
        )}
        </Loaded>
      </div>
    </div>
  )
}

// Карточка клиента: права, платежи и приход.
function ClientCard({ me, id, onBack }: { me: Me; id: number; onBack: () => void }) {
  const [rights, setRights] = useState<Entitlement[]>([])
  const [payments, setPayments] = useState<Payment[]>([])
  const [packs, setPacks] = useState<Pack[]>([])
  const [income, setIncome] = useState({ purpose: 'subscription:month', rubles: '', note: '' })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  /** Отказ по полю суммы: стоит под полем, а не полосой наверху. */
  const [суммаНеТа, setСуммаНеТа] = useState('')
  const ключПопытки = useRef('')
  const [busy, setBusy] = useState(false)

  const canSell = me.permissions.includes('sales')

  const read = useCallback(async () => {
    // Четыре независимых чтения идут разом, а не в очередь. Ждать их
    // по одному незачем: ни одно не зависит от прежнего, и четыре
    // круга по сети складываются в задержку, которую оператор видит
    // на каждом открытии карточки.
    //
    // Витрина наборов — отдельным обещанием с собственным отказом:
    // права на наборы у продавца может не быть, и отказ по ней не
    // должен ронять карточку клиента целиком. Остальные три ронять
    // обязаны: карточка без прав и приходов — это не карточка.
    const [one, rights, payments, packs] = await Promise.all([
      api.client(id),
      api.clientRights(id),
      api.clientPayments(id),
      api.packs().catch(() => null),
    ])
    setRights(rights?.entitlements ?? [])
    setPayments(payments?.payments ?? [])
    setPacks(packs?.packs ?? [])
    return one
  }, [id])
  const opened = useResource(read, 'Карточка не прочитана')
  const reload = opened.reload
  const client = opened.state === 'ready' ? opened.value : null

  async function accept(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    setСуммаНеТа('')
    const kopecks = вКопейки(income.rubles)
    if (kopecks === null) {
      // Под полем, а не полосой наверху: полоса над формой из трёх полей
      // не говорит, о котором из них речь.
      setСуммаНеТа('Рублями и копейками: 1990 или 1990,00')
      return
    }
    // Ключ повторности рождается один раз на попытку и живёт до её
    // успеха.
    //
    // Прежде он складывался из врача, назначения, суммы и числа месяца —
    // и был одинаковым у двух РАЗНЫХ приходов. Врач, купивший второй
    // месяц в тот же день, получал в ответ «уже оформлен», второго месяца
    // не получал, а деньги за него были приняты: оператор читал уверенное
    // слово и не пересчитывал. Случайный ключ таких совпадений не даёт.
    //
    // Но он и не рождается заново на каждое нажатие: при обрыве связи
    // оператор нажимает ещё раз, и повторная попытка обязана попасть в
    // тот же ключ — иначе сервер оформит второй приход, а деньги были
    // одни. Потому ключ сбрасывается только после удавшегося ответа.
    if (ключПопытки.current === '') ключПопытки.current = crypto.randomUUID()
    setBusy(true)
    try {
      const out = await api.acceptPayment({
        accountId: id,
        purpose: income.purpose,
        kopecks,
        idemKey: ключПопытки.current,
        note: income.note,
      })
      await reload()
      setNote(
        out.repeated
          ? `Этот приход уже доехал (платёж № ${out.id}). Второй раз деньги не приняты.`
          : `Приход оформлен: платёж № ${out.id}, ${рублями(out.kopecks)}. Право выдано.`,
      )
      // Попытка закрыта — следующему приходу нужен свой ключ, иначе
      // второй платёж того же врача за тот же месяц сервер примет за
      // повтор первого.
      ключПопытки.current = ''
      setIncome({ ...income, rubles: '', note: '' })
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Приход не оформлен')
    } finally {
      setBusy(false)
    }
  }

  async function refund(payment: Payment) {
    setFailure('')
    setNote('')
    // Причина спрашивается ДО подтверждения, а не после: отказавшийся её
    // писать возврат не оформил, и лишнего вопроса ему задавать незачем.
    const why = askReason(
      `Почему возвращается платёж № ${payment.id} на ${рублями(payment.kopecks)}?`,
      'Это единственное поле, по которому возврат потом разбирают. ' +
        'Напишите так, чтобы через полгода было понятно без вас.',
    )
    if (why === null) return
    if (
      !confirmed(
        `Вернуть платёж № ${payment.id} на ${рублями(payment.kopecks)}?`,
        'Право, купленное этим платежом, будет отозвано. Отменить возврат нельзя.',
      )
    ) {
      return
    }
    setBusy(true)
    try {
      await api.refundPayment(payment.id, why)
      await reload()
      setNote(`Платёж № ${payment.id} возвращён, право по нему отозвано.`)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Возврат не оформлен')
    } finally {
      setBusy(false)
    }
  }

  async function block(blocked: boolean) {
    setFailure('')
    setNote('')
    // Спрашивается только закрытие: открыть вход обратно можно той же
    // кнопкой, и подтверждение у обратимого приучает отвечать «да» не
    // читая — а вместе с ним перестают читать и остальные вопросы.
    if (
      blocked &&
      !confirmed(
        client === null ? 'Закрыть вход врачу?' : `Закрыть вход врачу ${именем(client)}?`,
        'Он не сможет ни заниматься, ни вернуть доступ по почте, пока вход не откроют обратно.',
      )
    ) {
      return
    }
    try {
      await api.setClientBlocked(id, blocked)
      await reload()
      setNote(blocked ? 'Вход закрыт.' : 'Вход открыт.')
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Отметка не поставлена')
    }
  }

  if (client === null) {
    return (
      <div>
        <div className="page-head">
          <h2>Клиент</h2>
          <button onClick={onBack}>К клиентам</button>
        </div>
        <Loaded from={opened} while="Читаем карточку…">
          {() => null}
        </Loaded>
      </div>
    )
  }

  return (
    <div>
      <div className="page-head">
        <h2>{client.displayName || client.email || `Клиент № ${client.id}`}</h2>
        <button onClick={onBack}>К клиентам</button>
      </div>
      <p className="hint">
        № {client.id} · {client.email || 'почта не привязана'} ·{' '}
        {счётом(client.devices, 'устройство', 'устройства', 'устройств')} · заведён{' '}
        {датой(client.createdAt)}
        {client.lastSeen ? ` · был ${датой(client.lastSeen)}` : ' · ни разу не заходил'}
      </p>

      {failure && <Banner kind="error">{failure}</Banner>}
      {note && <Banner kind="success">{note}</Banner>}

      <div className="page-section">
        <h3>Права</h3>
        {rights.length === 0 ? (
          <p className="empty">Живых прав нет: платного доступа у этого врача сейчас нет.</p>
        ) : (
          <div className="list">
            {rights.map((right, i) => (
              <div key={`${right.kind}-${right.pack}-${i}`} className="list-row">
                <span>
                  {right.kind === 'pack' ? `набор «${right.pack}»` : 'подписка на весь корпус'}
                  <span className="tag">{ORIGIN[right.origin] ?? right.origin}</span>
                </span>
                <span className="muted">
                  {right.expiresAt ? `до ${датой(right.expiresAt)}` : 'бессрочно'}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="page-section">
        <h3>Платежи</h3>
        {payments.length === 0 ? (
          <p className="empty">Платежей нет.</p>
        ) : (
          <div className="list">
            {payments.map((payment) => (
              <div key={payment.id} className="list-row">
                <span>
                  {назначением(payment.purpose)}
                  <span className="tag">{PAYMENT_STATUS[payment.status] ?? payment.status}</span>
                  <span className="tag">{SOURCE[payment.source] ?? payment.source}</span>
                </span>
                <span className="row-tools">
                  <span className="muted">
                    {рублями(payment.kopecks)} · {датой(payment.at)}
                    {payment.by && ` · ${payment.by}`}
                  </span>
                  {canSell && payment.status === 'paid' && (
                    <button className="danger" onClick={() => refund(payment)} disabled={busy}>
                      Вернуть
                    </button>
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      {canSell && (
        <div className="page-section">
          <h3>Оформить приход</h3>
          <p className="hint">
            Приход подтверждается вне системы, и подписывается он вашим
            именем. Право выдаётся тем же действием: двух учётов, склеенных
            задним числом, здесь нет.
          </p>
          <form className="form-grid" onSubmit={accept}>
            <label className="form-row">
              <span className="fld-label">За что</span>
              <select
                className="fld-long"
                value={income.purpose}
                onChange={(e) => setIncome({ ...income, purpose: e.target.value })}
              >
                <option value="subscription:month">подписка на месяц</option>
                <option value="subscription:year">подписка на год</option>
                {packs.map((pack) => (
                  <option key={pack.slug} value={`pack:${pack.slug}`}>
                    набор «{pack.title}»
                  </option>
                ))}
              </select>
            </label>
            <label className="form-row">
              <span className="fld-label">Сколько, рублей</span>
              <input
                className="fld-short"
                value={income.rubles}
                onChange={(e) => {
                  setIncome({ ...income, rubles: e.target.value })
                  setСуммаНеТа('')
                }}
                placeholder="1990"
                aria-invalid={суммаНеТа !== ''}
                aria-describedby={суммаНеТа === '' ? undefined : 'приход-сумма-отказ'}
              />
              {суммаНеТа !== '' && (
                <span id="приход-сумма-отказ" className="fld-error fld-across" role="alert">
                  {суммаНеТа}
                </span>
              )}
            </label>
            <label className="form-row">
              <span className="fld-label">Чем подтверждён</span>
              <input
                className="fld-long"
                value={income.note}
                onChange={(e) => setIncome({ ...income, note: e.target.value })}
                placeholder="перевод от 19.09, чек №…"
              />
            </label>
            <div className="form-actions">
              <button className="primary" type="submit" disabled={busy}>
                Оформить приход
              </button>
            </div>
          </form>
        </div>
      )}

      <div className="page-section">
        <h3>Вход</h3>
        <p className="hint">
          Закрытый вход останавливает выдачу задач, но платежи, права и
          разборы остаются на месте: учётная запись не удаляется никогда.
          Отозвать оплаченное право закрытием входа нельзя — для этого есть
          возврат платежа.
        </p>
        <div className="form-actions">
          {/* Красным только закрытие: открыть вход обратно — не опасное
              действие, и одинаковый вид у обоих стёр бы разницу. */}
          <button
            className={client.blocked ? undefined : 'danger'}
            onClick={() => block(!client.blocked)}
            disabled={busy}
          >
            {client.blocked ? 'Открыть вход' : 'Закрыть вход'}
          </button>
        </div>
      </div>
    </div>
  )
}

// Как звать врача в списке. Имени и почты может не быть вовсе: запись
// заводится молча, при первом запуске, и спрашивать имя у человека,
// который ещё не понял, что ему предлагают, — верный способ его потерять.
function именем(one: Client): string {
  return one.displayName || one.email || `Врач № ${one.id}`
}

// Назначение платежа человеческими словами. Образец закрыт на сервере, и
// разбирается он здесь по тому же правилу: «pack:<метка>» либо
// «subscription:month|year».
function назначением(purpose: string): string {
  if (purpose.startsWith('pack:')) return `набор «${purpose.slice('pack:'.length)}»`
  if (purpose === 'subscription:month') return 'подписка на месяц'
  if (purpose === 'subscription:year') return 'подписка на год'
  return purpose
}


