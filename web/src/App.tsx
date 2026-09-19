import { useState } from 'react'

import { Login } from './Login'
import { Packs } from './Packs'
import { Reports } from './Reports'
import { Sales } from './Sales'
import { Workshop } from './Workshop'
import { SourceList } from './SourceList'
import { SourceScreen } from './SourceScreen'
import { api, setToken } from './api'
import type { Me } from './api'

// Разделы студии.
//
// Вкладка видна всем, а закрывает раздел право на сервере. Спрятанная
// вкладка при открытой ручке — подсказка, где искать, а не запрет:
// человек, открывший инструменты разработчика, увидит и адрес, и ответ.
// Поэтому прячется здесь только действие, которое всё равно отказало бы, и
// рядом сказано, почему его нет.
const SECTIONS = [
  { id: 'sources', title: 'Источники' },
  { id: 'packs', title: 'Наборы' },
  { id: 'sales', title: 'Продажи' },
  { id: 'reports', title: 'Отчёты' },
  { id: 'workshop', title: 'Мастерская' },
] as const

type Section = (typeof SECTIONS)[number]['id']

export function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [section, setSection] = useState<Section>('sources')
  const [openSource, setOpenSource] = useState<number | null>(null)

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
    <div className="app">
      <header className="app-head">
        <h1>Куратор — студия составителя</h1>
        <span>
          {me.displayName || me.login} <button onClick={leave}>Выйти</button>
        </span>
      </header>

      <nav className="tabs">
        {SECTIONS.map((one) => (
          <button
            key={one.id}
            className={one.id === section ? 'tab tab-here' : 'tab'}
            aria-current={one.id === section ? 'page' : undefined}
            onClick={() => {
              setSection(one.id)
              setOpenSource(null)
            }}
          >
            {one.title}
          </button>
        ))}
      </nav>

      {section === 'packs' ? (
        <Packs me={me} />
      ) : section === 'sales' ? (
        <Sales me={me} />
      ) : section === 'reports' ? (
        <Reports me={me} />
      ) : section === 'workshop' ? (
        <Workshop me={me} />
      ) : openSource === null ? (
        <SourceList me={me} onOpen={setOpenSource} />
      ) : (
        <SourceScreen me={me} id={openSource} onBack={() => setOpenSource(null)} />
      )}
    </div>
  )
}
