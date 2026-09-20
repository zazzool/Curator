import { sectionTitle, type SectionId } from '../sections'
import { Icon } from './Icon'

/**
 * Верхняя полоса.
 *
 * Навигации здесь нет намеренно: разделы живут в колонке слева. Полосе
 * осталось общее — где мы сейчас и выход.
 *
 * «Где мы сейчас» не заменяет заголовок раздела: тот стоит внутри
 * страницы и уезжает с прокруткой, а эта строка остаётся на месте и
 * отвечает на вопрос «куда я попал» тогда, когда колонка свёрнута в
 * полосу значков.
 *
 * Имя раздела берётся из того же перечня, что и колонка: две редакции
 * имён разошлись бы молча.
 */
export function Toolbar({
  section,
  crumb,
  onSignOut,
}: {
  section: SectionId
  /** Что открыто внутри раздела: название источника, имя набора. */
  crumb?: string
  onSignOut: () => void
}) {
  return (
    <header className="app-bar">
      <div className="app-bar-here">
        <span>{sectionTitle(section)}</span>
        {crumb && <span className="app-bar-crumb">· {crumb}</span>}
      </div>

      <button
        type="button"
        className="bar-button app-bar-exit"
        onClick={onSignOut}
        title="Выйти"
        aria-label="Выйти"
      >
        <Icon name="logout" size={15} />
      </button>
    </header>
  )
}
