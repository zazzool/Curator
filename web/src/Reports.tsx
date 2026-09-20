import { useCallback, useState } from 'react'

import { счётом } from './words'
import { api } from './api'
import { Loaded, useResource } from './useResource'
import type { CaseStats, Me } from './api'

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
  const [days, setDays] = useState(7)

  const canRead = me.permissions.includes('analytics')

  // Оба отчёта одним чтением: порознь они дали бы две полосы отказа на
  // одном экране, а составителю и одной достаточно, чтобы понять, что
  // отчётов сегодня не будет.
  const read = useCallback(async () => {
    const cases = await api.reportCases()
    const funnel = await api.reportEvents(days)
    return {
      floor: cases?.floor ?? 0,
      easy: cases?.easy ?? [],
      hard: cases?.hard ?? [],
      events: funnel?.events ?? [],
    }
  }, [days])
  const report = useResource(read, 'Отчёты не построены')

  return (
    <div>
      <div className="page-head">
        <h2>Отчёты</h2>
      </div>
      {!canRead && (
        <p className="hint">Отчёты читает тот, кому выдано право «отчёты».</p>
      )}

      <Loaded from={report} while="Строим отчёты…">
        {({ floor, easy, hard, events }) => (
          <>
            <div className="page-section">
              <h3>Крайние задачи</h3>
              <p className="hint">
                Задачи, которые разбирают все подряд, и те, которых не разбирает
                никто. Доле верят начиная с{' '}
                {счётом(floor, 'попытки', 'попыток', 'попыток')}: три попытки —
                это не приговор задаче, а три человека.
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
              {/* Пустой список здесь — это ответ, а не ожидание: прежде
                  экран говорил «Читаем…» и тогда, когда прочитал, и
                  тогда, когда читать не смог. Теперь «читаем» живёт
                  снаружи, и пусто здесь значит ровно пусто. */}
              {events.length === 0 ? (
                <p className="empty">
                  За этот срок событий нет: либо приложением ещё никто не
                  пользовался, либо телеметрия до сервера не доезжает.
                </p>
              ) : (
                <div className="list">
                  {events.map((one) => (
                    <div key={one.name} className="list-row">
                      <span>{one.title || one.name}</span>
                      <span className="muted">
                        {/* Голое число слева нечитаемо: «4 · 4 врача» не
                            говорит, что первое — это разы, а не что-то ещё. */}
                        {one.count} всего ·{' '}
                        {счётом(one.accounts, 'врач', 'врача', 'врачей')}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </Loaded>
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
