import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { рублями, вКопейки } from './Money'
import { датой, счётом } from './words'
import { ApiError, api } from './api'
import type { Client, Entitlement, Me, Pack, Payment, Price } from './api'

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
    <div>
      <Prices me={me} />
      <ClientSearch onOpen={setOpen} />
    </div>
  )
}

// Цены: что и почём стоит на витрине.
function Prices({ me }: { me: Me }) {
  const [prices, setPrices] = useState<Price[] | null>(null)
  const [packs, setPacks] = useState<Pack[]>([])
  const [draft, setDraft] = useState({ purpose: 'subscription:month', rubles: '', enabled: true })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')

  const canSell = me.permissions.includes('sales')

  const reload = useCallback(async () => {
    try {
      setPrices((await api.prices())?.prices ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Цены не прочитаны')
    }
    try {
      setPacks((await api.packs())?.packs ?? [])
    } catch {
      // Наборы нужны только для списка назначений. Без права на них
      // оператор всё равно назовёт подписку — раздел из-за этого молчать
      // не должен.
      setPacks([])
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  async function save(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    const kopecks = вКопейки(draft.rubles)
    if (kopecks === null) {
      setFailure('Сумма пишется рублями и копейками: 1990 или 1990,00')
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

      {failure && <p className="alarm">{failure}</p>}
      {note && <p className="done">{note}</p>}

      <div className="page-section">
        {prices === null ? (
          <p className="empty">Читаем цены…</p>
        ) : prices.length === 0 ? (
          <p className="empty">
            Цен нет ни одной: витрина ничего не предлагает, и купить нечего.
          </p>
        ) : (
          <div className="list">
            {prices.map((price) => (
              <div key={price.purpose} className="list-row">
                <span>
                  {назначением(price.purpose)}
                  {!price.enabled && <span className="kind">не продаётся</span>}
                </span>
                <span className="muted">{рублями(price.kopecks)}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      {canSell && (
        <form className="page-section form-grid" onSubmit={save}>
          <label className="form-row">
            <span className="fld-label">За что</span>
            <select
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
              value={draft.rubles}
              onChange={(e) => setDraft({ ...draft, rubles: e.target.value })}
              placeholder="1990"
            />
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
            <button className="primary" type="submit">
              Сохранить цену
            </button>
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
  const [clients, setClients] = useState<Client[] | null>(null)
  const [failure, setFailure] = useState('')

  const search = useCallback(async (q: string) => {
    setFailure('')
    try {
      setClients((await api.clients(q))?.clients ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Клиенты не прочитаны')
    }
  }, [])

  useEffect(() => {
    void search('')
  }, [search])

  return (
    <div>
      <div className="page-head">
        <h2>Клиенты</h2>
      </div>
      <form
        className="form-actions"
        onSubmit={(event) => {
          event.preventDefault()
          void search(query)
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

      {failure && <p className="alarm">{failure}</p>}

      <div className="page-section">
        {clients === null ? (
          <p className="empty">Читаем…</p>
        ) : clients.length === 0 ? (
          <p className="empty">
            {query
              ? 'По этому запросу никого. Проверьте почту — искать можно и по части её.'
              : 'Клиентов пока нет: учётная запись заводится при первом запуске приложения.'}
          </p>
        ) : (
          <div className="list">
            {clients.map((one) => (
              <button key={one.id} className="list-row" onClick={() => onOpen(one.id)}>
                <span>
                  {именем(one)}
                  {one.blocked && <span className="kind">вход закрыт</span>}
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
          </div>
        )}
      </div>
    </div>
  )
}

// Карточка клиента: права, платежи и приход.
function ClientCard({ me, id, onBack }: { me: Me; id: number; onBack: () => void }) {
  const [client, setClient] = useState<Client | null>(null)
  const [rights, setRights] = useState<Entitlement[]>([])
  const [payments, setPayments] = useState<Payment[]>([])
  const [packs, setPacks] = useState<Pack[]>([])
  const [income, setIncome] = useState({ purpose: 'subscription:month', rubles: '', note: '' })
  const [failure, setFailure] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const canSell = me.permissions.includes('sales')

  const reload = useCallback(async () => {
    try {
      setClient(await api.client(id))
      setRights((await api.clientRights(id))?.entitlements ?? [])
      setPayments((await api.clientPayments(id))?.payments ?? [])
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Карточка не прочитана')
    }
    try {
      setPacks((await api.packs())?.packs ?? [])
    } catch {
      setPacks([])
    }
  }, [id])

  useEffect(() => {
    void reload()
  }, [reload])

  async function accept(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setNote('')
    const kopecks = вКопейки(income.rubles)
    if (kopecks === null) {
      setFailure('Сумма пишется рублями и копейками: 1990 или 1990,00')
      return
    }
    setBusy(true)
    try {
      // Ключ повтора придумывается здесь и один раз на попытку: сервер
      // отвечает на повтор прежним платежом, а не вторым приходом.
      const out = await api.acceptPayment({
        accountId: id,
        purpose: income.purpose,
        kopecks,
        idemKey: `студия-${id}-${income.purpose}-${kopecks}-${new Date().toISOString().slice(0, 10)}`,
        note: income.note,
      })
      await reload()
      setNote(
        out.repeated
          ? `Такой приход уже оформлен сегодня (платёж № ${out.id}). Второй раз деньги не приняты.`
          : `Приход оформлен: платёж № ${out.id}, ${рублями(out.kopecks)}. Право выдано.`,
      )
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
    setBusy(true)
    try {
      await api.refundPayment(payment.id, 'возврат оформлен в студии')
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
        {failure ? <p className="alarm">{failure}</p> : <p className="empty">Читаем карточку…</p>}
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

      {failure && <p className="alarm">{failure}</p>}
      {note && <p className="done">{note}</p>}

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
                  <span className="kind">{ORIGIN[right.origin] ?? right.origin}</span>
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
                  <span className="kind">{PAYMENT_STATUS[payment.status] ?? payment.status}</span>
                  <span className="kind">{SOURCE[payment.source] ?? payment.source}</span>
                </span>
                <span className="row-tools">
                  <span className="muted">
                    {рублями(payment.kopecks)} · {датой(payment.at)}
                    {payment.by && ` · ${payment.by}`}
                  </span>
                  {canSell && payment.status === 'paid' && (
                    <button onClick={() => refund(payment)} disabled={busy}>
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
                value={income.rubles}
                onChange={(e) => setIncome({ ...income, rubles: e.target.value })}
                placeholder="1990"
              />
            </label>
            <label className="form-row">
              <span className="fld-label">Чем подтверждён</span>
              <input
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
          <button onClick={() => block(!client.blocked)} disabled={busy}>
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


