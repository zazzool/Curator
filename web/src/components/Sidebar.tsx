import { GROUPS, type SectionId } from '../sections'
import { Icon } from './Icon'

/**
 * Боковая колонка разделов.
 *
 * Разделы стояли строкой вкладок наверху. Строка держит их, пока их
 * полдюжины: на первом же десятом она превращается в набор раскрывающихся
 * списков, и попасть в раздел можно, только зная, под каким словом он
 * лежит. Колонка этого ограничения не имеет — разделов в ней видно
 * столько, сколько есть, и место каждого объяснимо заголовком группы.
 *
 * Возражение против колонки известно: двести пикселей она отнимает у
 * текста источника. Снято оно не доводом, а устройством — колонка
 * сворачивается в полосу значков, и свёрнутой её помнят между заходами.
 *
 * Заголовок группы не нажимается: своего содержимого у группы нет, и
 * переход по её имени вёл бы в один из разделов наугад.
 */
export function Sidebar({
  section,
  onGo,
  collapsed,
  onCollapsed,
}: {
  /** Раздел открытого адреса; пусто — ненайденная страница, и подсвечивать нечего. */
  section: SectionId | null
  onGo: (to: SectionId) => void
  collapsed: boolean
  onCollapsed: (next: boolean) => void
}) {
  return (
    <nav className={`app-nav${collapsed ? ' collapsed' : ''}`} aria-label="Разделы">
      <button
        type="button"
        className="nav-toggle"
        aria-pressed={collapsed}
        aria-label={collapsed ? 'Развернуть меню' : 'Свернуть меню'}
        title={collapsed ? 'Развернуть меню' : 'Свернуть меню'}
        onClick={() => onCollapsed(!collapsed)}
      >
        <Icon name="manage" size={16} />
        <span className="nav-label">Разделы</span>
      </button>

      {GROUPS.map((group) => (
        <div className="nav-group" key={group.label}>
          <div className="nav-group-label">{group.label}</div>
          {group.sections.map((one) => {
            const here = one.id === section
            return (
              <button
                key={one.id}
                type="button"
                className={`nav-item${here ? ' active' : ''}`}
                aria-current={here ? 'page' : undefined}
                // Подпись в свёрнутом виде остаётся только всплывающей:
                // полоса значков без неё — загадка, а не меню.
                title={one.hint}
                onClick={() => onGo(one.id)}
              >
                <Icon name={one.icon} size={15} />
                <span className="nav-label">{one.title}</span>
              </button>
            )
          })}
        </div>
      ))}
    </nav>
  )
}
