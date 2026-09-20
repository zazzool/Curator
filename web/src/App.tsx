import { Suspense, lazy, useEffect, useState } from 'react'

import { Login } from './Login'
import { NotFound } from './NotFound'
import { SourceList } from './SourceList'
import { SourceScreen } from './SourceScreen'
import { ErrorBoundary } from './components/ErrorBoundary'
import { Sidebar } from './components/Sidebar'
import { IdentityBar } from './components/IdentityBar'
import { StatusBar } from './components/StatusBar'
import { Toolbar } from './components/Toolbar'
import { readSidebarCollapsed, writeSidebarCollapsed } from './sidebarState'
import { go, routePath, sectionOf, sectionRoute, useRoute } from './router'
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
const Audiences = lazy(() => import('./Audiences').then((m) => ({ default: m.Audiences })))
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
 *
 * **Где мы — в адресе, а не в состоянии этого экрана.** Раздел и открытый
 * источник лежали здесь в `useState`, и от этого не работало ровно то,
 * ради чего адрес существует: ссылки на источник не было вовсе, «назад»
 * браузера выкидывало из студии целиком, а F5 возвращал на список
 * источников, чем бы составитель ни был занят до того. Разбор адреса — в
 * `router.ts`, и он один на всю студию.
 */
export function App() {
  const [me, setMe] = useState<Me | null>(null)
  const route = useRoute()
  const section = sectionOf(route)
  /**
   * Название открытого источника — для верхней полосы.
   *
   * В адресе стоит только номер, а полоса рисуется раньше, чем экран
   * источника успеет прочитать название. Пришедший по прямой ссылке видит
   * полосу без него первую секунду, и это честно: названия ещё никто не
   * знает. Пришедший из списка видит его сразу — список название уже
   * прочитал и отдаёт вместе с переходом.
   */
  const [crumb, setCrumb] = useState('')
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
  /**
   * Спрашиваем ли ещё сервер, открыта ли сессия из печенья.
   *
   * Отдельное состояние, а не «пока нет me — показываем вход»: ответ
   * приходит через мгновение, и за это мгновение вошедший успел бы
   * увидеть форму входа и начать набирать имя.
   */
  const [asking, setAsking] = useState(true)

  // Слой обращений не знает ни о состоянии, ни о входе — и не должен:
  // иначе он потянул бы за собой половину студии. Он лишь зовёт того,
  // кого здесь привязали, когда сервер ответил «сессии нет».
  useEffect(() => {
    setAuthLost(() => {
      setMe(null)
      setCrumb('')
      setLost('Сессия кончилась. Войдите заново.')
    })
    return () => setAuthLost(null)
  }, [])

  /**
   * Возврат к открытой сессии при загрузке страницы.
   *
   * Токен студии живёт в памяти страницы, и до печенья сессии F5, закрытая
   * вкладка и уснувший ноутбук выбрасывали на вход одинаково — по десять
   * раз на дню, при какой угодно сессии на сервере. Спрашивается именно
   * сервер: печенье HttpOnly, и студия его не видит.
   *
   * Отказ связи молча не глотается, но и на вход не ругается: показывается
   * форма, а сказать «сервер не отвечает» ей есть чем — первое же обращение
   * оттуда скажет это точнее.
   */
  useEffect(() => {
    let alive = true
    void api
      .resume()
      .then((who) => {
        if (alive && who) setMe(who)
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) setAsking(false)
      })
    return () => {
      alive = false
    }
  }, [])

  // Пустая страница на время вопроса, а не «Читаем…»: вопрос идёт к своему
  // же серверу и укладывается в мгновение, а надпись, мелькнувшая и
  // пропавшая, читается как сбой.
  if (asking) return null

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
    // Выход ЖДЁТ ответа сервера, и это изменилось вместе с печеньем.
    // Пока токен жил только в памяти страницы, стереть его здесь и было
    // выходом. Теперь сессию держит печенье, погасить которое может один
    // сервер: не дождись мы его — студия выглядела бы закрытой, а первая
    // же перезагрузка возвращала бы в неё без кода.
    //
    // Местное состояние стирается в любом случае: человек нажал «выйти», и
    // на этом устройстве он вышел. Не удалось закрыть сессию — об этом
    // говорится прямо, потому что нажать «выйти» ещё раз он сможет, только
    // зная, что первый раз не сработал.
    let closed = true
    try {
      await api.logout()
    } catch {
      closed = false
    }
    setToken('')
    setMe(null)
    setCrumb('')
    go({ name: 'sources' })
    setLost(closed ? '' : 'Вышли на этом устройстве, но связи с сервером не было. Выйдите ещё раз.')
  }

  return (
    <div className="app-frame">
      <Sidebar
        section={section}
        onGo={(to) => {
          setCrumb('')
          go(sectionRoute(to))
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
          crumb={route.name === 'source' ? crumb || undefined : undefined}
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
          <ErrorBoundary key={routePath(route)}>
            {/* Ожидание названо теми же словами, что и всякое чтение в
                студии: человеку всё равно, ждёт он файл раздела или
                ответ сервера. Граница отказа стоит СНАРУЖИ — не
                приехавший по обрыву кусок раздела это отказ отрисовки,
                и без границы он снял бы всё дерево белым экраном. */}
            <Suspense fallback={<p className="empty">Читаем…</p>}>
              {route.name === 'packs' ? (
                <Packs me={me} />
              ) : route.name === 'sales' ? (
                <Sales me={me} />
              ) : route.name === 'audiences' ? (
                <Audiences me={me} />
              ) : route.name === 'reports' ? (
                <Reports me={me} />
              ) : route.name === 'workshop' ? (
                <Workshop me={me} />
              ) : route.name === 'source' ? (
                <SourceScreen
                  me={me}
                  id={route.id}
                  onTitle={setCrumb}
                  onBack={() => {
                    setCrumb('')
                    go({ name: 'sources' })
                  }}
                />
              ) : route.name === 'unknown' ? (
                <NotFound path={route.path} />
              ) : (
                <SourceList
                  me={me}
                  onOpen={(id, title) => {
                    setCrumb(title)
                    go({ name: 'source', id })
                  }}
                />
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
