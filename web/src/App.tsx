import { useState } from 'react'

import { Login } from './Login'
import { Packs } from './Packs'
import { Reports } from './Reports'
import { Sales } from './Sales'
import { Workshop } from './Workshop'
import { SourceList } from './SourceList'
import { SourceScreen } from './SourceScreen'
import { Sidebar } from './components/Sidebar'
import { IdentityBar } from './components/IdentityBar'
import { StatusBar } from './components/StatusBar'
import { Toolbar } from './components/Toolbar'
import { readSidebarCollapsed, writeSidebarCollapsed } from './sidebarState'
import type { SectionId } from './sections'
import { api, setToken } from './api'
import type { Me } from './api'

/**
 * Каркас студии: колонка разделов слева и три полосы справа от неё —
 * полоса действий, рабочая область, строка состояния.
 *
 * Устроено как в настольных редакторах и по той же причине: инструменты и
 * состояние не должны уезжать вместе с содержимым. Заголовок раздела
 * уезжает вверх с первой же прокруткой, а «где я» и «кто я» нужны в любой
 * момент работы.
 *
 * Разделов в верхней полосе нет намеренно. Строкой вкладок они и стояли,
 * и строка держит их, пока их полдюжины; колонка не упирается в это, а
 * платы за неё нет — она сворачивается в полосу значков, и свёрнутой её
 * помнят между заходами.
 *
 * Перечень разделов сюда не переписан: он лежит в `sections.ts`, откуда
 * его читают и колонка, и верхняя полоса. Две редакции имён разошлись бы
 * молча.
 */
export function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [section, setSection] = useState<SectionId>('sources')
  // Открытый источник: опознаватель нужен экрану, название — верхней
  // полосе. Спрашивать название у экрана нельзя: он читает источник сам и
  // отвечает позже, чем полоса рисуется.
  const [openSource, setOpenSource] = useState<{ id: number; title: string } | null>(null)
  /**
   * Свёрнута ли колонка. Начальное значение читается из браузера один раз,
   * при первом построении: составитель, свернувший колонку, не должен
   * сворачивать её заново на каждой странице.
   */
  const [navCollapsed, setNavCollapsed] = useState(readSidebarCollapsed)

  if (!me) return <Login onEnter={setMe} />

  async function leave() {
    // Выход не ждёт ответа сервера: человек нажал «выйти» и должен выйти.
    // Сессия на сервере закрывается тем же обращением, а его отказ здесь
    // ничего не меняет — показывать его незачем.
    void api.logout().catch(() => undefined)
    setToken('')
    setMe(null)
    setOpenSource(null)
    setSection('sources')
  }

  return (
    <div className="app-frame">
      <Sidebar
        section={section}
        onGo={(to) => {
          setSection(to)
          setOpenSource(null)
        }}
        collapsed={navCollapsed}
        onCollapsed={(next) => {
          setNavCollapsed(next)
          writeSidebarCollapsed(next)
        }}
      />

      <div className="app">
        <Toolbar
          section={section}
          crumb={section === 'sources' ? openSource?.title : undefined}
          onSignOut={leave}
        />

        <main className="main stack">
          {/* Кто вошёл — первой строкой рабочей области и на каждом экране,
              включая экран отказа: заголовок раздела уезжает вверх с первой
              же прокруткой, а «кто я» нужно в любой момент работы. */}
          <IdentityBar who={me.displayName || me.login} permissions={me.permissions} />

          {section === 'packs' ? (
            <Packs me={me} />
          ) : section === 'sales' ? (
            <Sales me={me} />
          ) : section === 'reports' ? (
            <Reports me={me} />
          ) : section === 'workshop' ? (
            <Workshop me={me} />
          ) : openSource === null ? (
            <SourceList me={me} onOpen={(id, title) => setOpenSource({ id, title })} />
          ) : (
            <SourceScreen me={me} id={openSource.id} onBack={() => setOpenSource(null)} />
          )}
        </main>

        {/* Полоса ждёт сведений об открытом экране и пока пуста — пустая
            прячется сама, правилом в разметке. */}
        <StatusBar />
      </div>
    </div>
  )
}
