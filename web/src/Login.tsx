import { useEffect, useRef, useState, type FormEvent } from 'react'

import { ApiError, api, setToken } from './api'
import type { Me } from './api'

// Длина одноразового кода.
//
// Второе место для серверного totp.Digits? Нет: шесть цифр задал RFC 6238,
// и их же ждёт всякий аутентификатор — это не настройка, которую однажды
// поменяют, а форма самой вещи. Форме число нужно своё: им ограничен ввод и
// им же решается, когда отправить код без нажатия.
const CODE_LENGTH = 6

/**
 * Вход в студию: имя и одноразовый код.
 *
 * # Страница молчит о себе
 *
 * До входа браузеру показывать нечего, и всё, что форма сообщает о системе
 * сверх необходимого, — ответ на вопрос, который задаёт только посторонний.
 * Поэтому здесь нет ни пояснения про аутентификатор, ни срока жизни кода,
 * ни совета «дождитесь следующего»: первое называет, что именно красть,
 * второе — в каком окне укладываться, третье — как устроена сверка. Всё
 * это человек, у которого аутентификатор есть, и так знает.
 *
 * По той же причине отказ один на все случаи, и приходит он с сервера:
 * разные слова на «нет такого имени» и «код не подошёл» — это ответ на
 * вопрос, существует ли имя.
 *
 * # Один шаг, а не два
 *
 * Разводить имя и код по двум шагам — нынешняя мода, и здесь она вредна:
 * второй шаг показывается либо всем подряд (тогда шаг лишний), либо только
 * известному имени — а это и есть перечисление имён, только с нарядным
 * переходом. Два поля сразу дешевле и не отвечают ни на чей вопрос.
 *
 * # Отказ показывается на месте
 *
 * Человек, ошибшийся кодом, должен исправить шесть цифр, а не искать, куда
 * делась форма.
 */
export function Login({
  onEnter,
  notice,
}: {
  onEnter: (me: Me) => void
  /**
   * Почему человек снова здесь, если он уже входил. Пустая строка —
   * обычный вход.
   *
   * Без этой строки истёкшая сессия выглядела как самопроизвольный
   * выход: только что была студия, теперь просят код, и непонятно,
   * сломалось что-то или так задумано.
   */
  notice?: string
}) {
  const [login, setLogin] = useState('')
  const [code, setCode] = useState('')
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)
  const codeField = useRef<HTMLInputElement>(null)

  const ready = login.trim() !== '' && code.length === CODE_LENGTH

  // Курсор возвращается в поле кода после отказа — отдельным действием, а
  // не строкой в обработчике: пока идёт проверка, поле недоступно, а
  // недоступное поле курсора не принимает, и фокус молча не случился бы.
  useEffect(() => {
    if (failure !== '' && !busy) codeField.current?.focus()
  }, [failure, busy])

  async function enter(name: string, digits: string) {
    if (busy) return
    setFailure('')
    setBusy(true)
    try {
      const { token } = await api.login(name.trim(), digits)
      setToken(token)
      onEnter(await api.me())
    } catch (error) {
      setFailure(error instanceof ApiError ? error.message : 'Сервер не отвечает')
      // Имя остаётся, стирается только код. Код одноразовый и к этой
      // секунде устарел в любом случае, а имя набрано верно — стирать его
      // значит наказывать за чужую ошибку.
      setCode('')
    } finally {
      setBusy(false)
    }
  }

  // Набранный до конца код уходит сам, без нажатия.
  //
  // Шесть цифр — это вся форма: дожидаться отдельного нажатия после
  // последней цифры незачем. Вставка кода целиком попадает сюда же и
  // отправляется тем же путём; нецифры отбрасываются, потому что из
  // аутентификатора код копируют вместе с пробелом посередине.
  function typeCode(raw: string) {
    const digits = raw.replace(/\D/g, '').slice(0, CODE_LENGTH)
    setCode(digits)
    if (digits.length === CODE_LENGTH && login.trim() !== '') void enter(login, digits)
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    void enter(login, code)
  }

  return (
    <main className="gate">
      <form className="gate-card card" onSubmit={submit} aria-busy={busy}>
        {/* Полоса ожидания, а не только слово на кнопке: отказ входа
            приходит не сразу — сервер придерживает ответ тем дольше, чем
            больше было неудач подряд, — и несколько секунд неподвижной
            формы человек читает как «сломалось». */}
        {busy && <div className="gate-wait" aria-hidden="true" />}

        <div className="gate-head">
          <span className="gate-mark" aria-hidden="true">
            К
          </span>
          <span className="gate-name">Куратор</span>
        </div>

        <div className="stack">
          {notice && (
            <p className="banner" role="status">
              {notice}
            </p>
          )}

          <div>
            <label htmlFor="gate-login">Имя</label>
            <input
              id="gate-login"
              name="login"
              value={login}
              autoFocus
              // Хранителю паролей поле называется, а не прячется: имя
              // входа не секрет, а подставленное им имя избавляет от
              // опечатки в единственном поле, где опечатку не видно.
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              disabled={busy}
              onChange={(e) => setLogin(e.target.value)}
            />
          </div>

          <div>
            <label htmlFor="gate-code">Код</label>
            <input
              id="gate-code"
              name="code"
              ref={codeField}
              className="gate-code"
              value={code}
              // Не type="number": ведущий ноль там пропадает, а 000123 и
              // 123 — разные коды. Цифровую клавиатуру телефона вызывает
              // inputMode, и делает это, не трогая разбор значения.
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={CODE_LENGTH}
              enterKeyHint="go"
              // Отказ привязан к полю, а не просто нарисован рядом: иначе
              // читающий с экрана слышит его один раз при появлении и
              // больше не находит, вернувшись в поле.
              aria-describedby={failure ? 'gate-code-shape gate-failure' : 'gate-code-shape'}
              aria-invalid={failure !== ''}
              disabled={busy}
              onChange={(e) => typeCode(e.target.value)}
            />
            {/* Длина кода видна глазами — по тому, как заполняется поле, —
                а читающему с экрана видеть нечего. Сказано только о форме
                ввода, которая и так объявлена в разметке поля; про то,
                откуда код берётся, страница молчит. */}
            <span id="gate-code-shape" className="visually-hidden">
              Шесть цифр
            </span>
          </div>

          {/* Отказ стоит под полем кода, а не под кнопкой: исправлять
              человеку именно код, и сообщение должно попасться ему по
              дороге к полю, а не остаться за спиной. */}
          {failure && (
            <p className="banner error" id="gate-failure" role="alert">
              {failure}
            </p>
          )}

          {/* Кнопка недоступна, пока форма не заполнена. Полей два, и
              незаполненное видно без подсказки; зато каждое пустое
              нажатие — это ещё одна неудачная попытка, после которой
              сервер придержит следующий ответ. */}
          <button className="primary gate-enter" type="submit" disabled={!ready || busy}>
            {busy ? 'Проверяем…' : 'Войти'}
          </button>
        </div>
      </form>
    </main>
  )
}
