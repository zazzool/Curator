import { Suspense, lazy, useEffect, useState } from 'react'

import { Login } from './Login'
import { SourceList } from './SourceList'
import { SourceScreen } from './SourceScreen'
import { ErrorBoundary } from './components/ErrorBoundary'
import { Sidebar } from './components/Sidebar'
import { IdentityBar } from './components/IdentityBar'
import { StatusBar } from './components/StatusBar'
import { Toolbar } from './components/Toolbar'
import { readSidebarCollapsed, writeSidebarCollapsed } from './sidebarState'
import type { SectionId } from './sections'
import { api, setAuthLost, setToken } from './api'
import type { Me } from './api'

/*
 * Четыре раздела приезжают отдельными кусками, а не вместе со студией.
 *
 * Собранное одним куском, оно весило под триста килобайт, и всё это
 * скачивала СТРАНИЦА ВХОДА: Продажи, Отчёты, Наборы и Мастерская — до
 * того, как станет известно, есть ли у вошедшего право хоть на один из
 * них. Составитель без права продаж не открывает Продажи никогда, а
 * платит за них при каждом входе.
 *
 * Источники и экран источника остаются в общем куске намеренно: это
 * раздел по умолчанию, и подгружать его отдельно значит задержать ровно
 * тот экран, который открывается сразу после входа.
 */
const Packs = lazy(() => import('./Packs').then((m) => ({ default: m.Packs })))
const Reports = lazy(() => import('./Reports').then((m) => ({ default: m.Reports })))
const Sales = lazy(() => import('./Sales').then((m) => ({ default: m.Sales })))
const Workshop = lazy(() => import('./Workshop').then((m) => ({ default: m.Workshop })))

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
  /**
   * Почему студия закрылась сама. Пустая строка — человек ещё не входил
   * или вышел сам.
   */
  const [lost, setLost] = useState('')

  // Слой обращений не знает ни о состоянии, ни о входе — и не должен:
  // иначе он потянул бы за собой половину студии. Он лишь зовёт того,
  // кого здесь привязали, когда сервер ответил «сессии нет».
  useEffect(() => {
    setAuthLost(() => {
      setMe(null)
      setOpenSource(null)
      setLost('Сессия кончилась. Войдите заново.')
    })
    return () => setAuthLost(null)
  }, [])

  if (!me) {
    return (
      <Login
        onEnter={(who) => {
          setLost('')
          setMe(who)
        }}
        notice={lost}
      />
    )
  }

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

          {/* Граница отказа охватывает только рабочую область: кривая
              задача должна стоить одной панели, а не всей студии вместе с
              колонкой разделов и именем вошедшего. Ключ сбрасывает
              упавшее при переходе — иначе отказ на одном экране висел бы
              и на исправных. */}
          <ErrorBoundary key={`${section}:${openSource?.id ?? ''}`}>
            {/* Ожидание названо теми же словами, что и всякое чтение в
                студии: человеку всё равно, ждёт он файл раздела или
                ответ сервера. Граница отказа стоит СНАРУЖИ — не
                приехавший по обрыву кусок раздела это отказ отрисовки,
                и без границы он снял бы всё дерево белым экраном. */}
            <Suspense fallback={<p className="empty">Читаем…</p>}>
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
            </Suspense>
          </ErrorBoundary>
        </main>

        {/* Полоса ждёт сведений об открытом экране и пока пуста — пустая
            прячется сама, правилом в разметке. */}
        <StatusBar />
      </div>
    </div>
  )
}
