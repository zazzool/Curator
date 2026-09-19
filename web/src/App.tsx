import { useState } from 'react'

import { Login } from './Login'
import { SourceList } from './SourceList'
import { SourceScreen } from './SourceScreen'
import { api, setToken } from './api'
import type { Me } from './api'

export function App() {
  const [me, setMe] = useState<Me | null>(null)
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
  }

  return (
    <div className="app">
      <header className="app-head">
        <h1>Куратор — студия составителя</h1>
        <span>
          {me.displayName || me.login} <button onClick={leave}>Выйти</button>
        </span>
      </header>
      {openSource === null ? (
        <SourceList me={me} onOpen={setOpenSource} />
      ) : (
        <SourceScreen me={me} id={openSource} onBack={() => setOpenSource(null)} />
      )}
    </div>
  )
}
