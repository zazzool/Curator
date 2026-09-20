import { useCallback, useEffect, useState } from 'react'

import { счётом } from './words'
import { ApiError, api } from './api'
import type { CaseStats, EventCount, Me } from './api'

// Отчёты: как разбирают задачи и что делают в приложении.
//
// Раздел читает поведение врачей, и закрыт он отдельным правом: смотреть,
// как люди ошибаются, и править задачи — разные занятия.
//
// Два отчёта, и оба про крайности. Средняя задача составителю не нужна:
// ему нужны те, которые разбирают все подряд (значит, спрашивать было не о
// чем), и те, которых не разбирает никто (значит, задача неразрешима или
// неверна). Список всех задач по решаемости — та же стена чисел, в которой
// крайности и тонут.

export function Reports({ me }: { me: Me }) {
  const [floor, setFloor] = useState(0)
  const [easy, setEasy] = useState<CaseStats[]>([])
  const [hard, setHard] = useState<CaseStats[]>([])
  const [events, setEvents] = useState<EventCount[] | null>(null)
  const [days, setDays] = useState(7)
  const [failure, setFailure] = useState('')
  const [read, setRead] = useState(false)

  const canRead = me.permissions.includes('analytics')

  const reload = useCallback(async () => {
    setFailure('')
    try {
      const cases = await api.reportCases()
      setFloor(cases?.floor ?? 0)
      setEasy(cases?.easy ?? [])
      setHard(cases?.hard ?? [])
      const funnel = await api.reportEvents(days)
      setEvents(funnel?.events ?? [])
      setRead(true)
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Отчёты не построены')
    }
  }, [days])

  useEffect(() => {
    void reload()
  }, [reload])

  return (
    <div>
      <div className="page-head">
        <h2>Отчёты</h2>
      </div>
      {!canRead && (
        <p className="hint">Отчёты читает тот, кому выдано право «отчёты».</p>
      )}
      {failure && <p className="banner error">{failure}</p>}

      <div className="page-section">
        <h3>Крайние задачи</h3>
        <p className="hint">
          Задачи, которые разбирают все подряд, и те, которых не разбирает
          никто. Доле верят начиная с {счётом(floor, 'попытки', 'попыток', 'попыток')}:
          три попытки — это не приговор задаче, а три человека.
        </p>

        <h4 className="group-title">Слишком лёгкие</h4>
        <StatsList list={easy} empty="Слишком лёгких задач нет." />

        <h4 className="group-title">Почти неразрешимые</h4>
        <StatsList
          list={hard}
          empty="Неразрешимых задач нет."
          hint="Сначала проверьте разметку: задача, у которой верным помечен не тот вариант, выглядит ровно так."
        />
      </div>

      <div className="page-section">
        <h3>Что делают в приложении</h3>
        <div className="form-actions">
          {[7, 30, 90].map((n) => (
            <button
              key={n}
              className={n === days ? 'tab tab-here' : 'tab'}
              onClick={() => setDays(n)}
            >
              {счётом(n, 'день', 'дня', 'дней')}
            </button>
          ))}
        </div>
        {events === null ? (
          <p className="empty">Читаем…</p>
        ) : events.length === 0 ? (
          <p className="empty">
            {read
              ? 'За этот срок событий нет: либо приложением ещё никто не пользовался, либо телеметрия до сервера не доезжает.'
              : 'Читаем…'}
          </p>
        ) : (
          <div className="list">
            {events.map((one) => (
              <div key={one.name} className="list-row">
                <span>{one.title || one.name}</span>
                <span className="muted">
                  {/* Голое число слева нечитаемо: «4 · 4 врача» не
                      говорит, что первое — это разы, а не что-то ещё. */}
                  {one.count} всего · {счётом(one.accounts, 'врач', 'врача', 'врачей')}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function StatsList({
  list,
  empty,
  hint,
}: {
  list: CaseStats[]
  empty: string
  hint?: string
}) {
  if (list.length === 0) return <p className="empty">{empty}</p>
  return (
    <>
      {hint && <p className="hint">{hint}</p>}
      <div className="list">
        {list.map((one) => (
          <div key={one.caseId} className="list-row">
            <span>
              <span className="mono">{one.caseId}</span>
              {/* Ненадёжной доле стоит метка, а не звёздочка внизу: сноску
                  внизу списка читают не глядя на строку, ради которой она
                  и поставлена. */}
              {!one.enough && <span className="tag">попыток мало</span>}
            </span>
            <span className="muted">
              {долей(one.solveRate)} верных из{' '}
              {счётом(one.attempts, 'попытки', 'попыток', 'попыток')}
            </span>
          </div>
        ))}
      </div>
    </>
  )
}

// Доля показывается процентами без дробей: «73,4 %» составитель читает как
// точность, которой у доли по сорока попыткам нет.
function долей(rate: number): string {
  return `${Math.round(rate * 100)} %`
}
