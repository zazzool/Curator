import { useState, type FormEvent } from 'react'

import { ApiError, api, setToken } from './api'
import type { Me } from './api'

// Вход в студию: имя и одноразовый код.
//
// Карточка посреди пустой страницы, а не страница с заголовком: до входа
// браузеру показывать нечего, и всё, что форма сообщает о системе сверх
// нужного, — ответ на вопрос, который задаёт только посторонний.
//
// Отказ показывается на месте, а не уводит на другую страницу: человек,
// ошибшийся кодом, должен исправить шесть цифр, а не искать, куда делась
// форма.
export function Login({ onEnter }: { onEnter: (me: Me) => void }) {
  const [login, setLogin] = useState('')
  const [code, setCode] = useState('')
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setFailure('')
    setBusy(true)
    try {
      const { token } = await api.login(login.trim(), code.trim())
      setToken(token)
      onEnter(await api.me())
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Сервер не отвечает')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="gate card stack" onSubmit={submit}>
      <div>
        <label htmlFor="gate-login">Имя</label>
        <input
          id="gate-login"
          name="login"
          value={login}
          autoFocus
          autoComplete="off"
          disabled={busy}
          onChange={(e) => setLogin(e.target.value)}
        />
      </div>

      <div>
        <label htmlFor="gate-code">Код</label>
        <input
          id="gate-code"
          name="code"
          value={code}
          inputMode="numeric"
          autoComplete="one-time-code"
          disabled={busy}
          onChange={(e) => setCode(e.target.value)}
        />
        <p className="hint">
          Код на шесть цифр показывает программа-аутентификатор. Код живёт
          полминуты — если он не подошёл, дождитесь следующего.
        </p>
      </div>

      <button className="primary" type="submit" disabled={busy}>
        {busy ? 'Проверяем…' : 'Войти'}
      </button>

      {failure && <p className="banner error">{failure}</p>}
    </form>
  )
}
