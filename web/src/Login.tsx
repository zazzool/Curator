import { useState, type FormEvent } from 'react'

import { ApiError, api, setToken } from './api'
import type { Me } from './api'

// Вход в студию: имя и одноразовый код.
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
    <form className="login" onSubmit={submit}>
      <div className="page-head">
        <h2>Вход в студию</h2>
      </div>
      <p className="hint">
        Код на шесть цифр показывает программа-аутентификатор. Код живёт
        полминуты — если он не подошёл, дождитесь следующего.
      </p>
      <div className="form-grid">
        <label className="form-row">
          <span className="fld-label">Имя</span>
          <input value={login} onChange={(e) => setLogin(e.target.value)} autoFocus />
        </label>
        <label className="form-row">
          <span className="fld-label">Код</span>
          <input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
          />
        </label>
        <div className="form-actions">
          <button className="primary" type="submit" disabled={busy}>
            {busy ? 'Проверяем…' : 'Войти'}
          </button>
        </div>
      </div>
      {failure && <p className="alarm">{failure}</p>}
    </form>
  )
}
