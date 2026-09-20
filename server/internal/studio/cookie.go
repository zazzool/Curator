package studio

import (
	"net/http"
	"strings"
)

// Печенье сессии студии.
//
// # Зачем оно нужно
//
// Тридцатисуточный срок сессии на сервере ничего не даёт, пока токен живёт
// только в памяти страницы: F5, закрытая вкладка, уснувший ноутбук — и
// студия снова просит код. Именно это составитель и называет «постоянным
// разлогином», а не истёкшую сессию: та случается раз в месяц, а перезагрузка
// страницы — по десять раз на дню.
//
// # Почему печенье, а не хранилище браузера
//
// Токен, положенный в localStorage, читается любым сценарием на странице, и
// украденный — уезжает наружу и работает месяц откуда угодно. Печенье с
// HttpOnly сценарию не видно вовсе: чужой код может ходить им из браузера
// хозяина, но унести его с собой не может.
//
// SameSite=Strict — вместо отдельной защиты от подделки межсайтового
// запроса: студия и её API на одном домене, и печенье, не уезжающее с чужого
// сайта, закрывает этот вопрос целиком. Path=/admin — чтобы оно не ходило
// к `/v1`, которому до него дела нет.
const sessionCookie = "curator_studio"

// setSession кладёт токен в печенье.
//
// MaxAge берётся из SessionTTL, а не пишется числом: два места для одного
// срока расходятся молча, и разойдясь, дали бы браузер, забывший вход
// раньше сервера, — то есть ровно ту беду, ради которой печенье и заведено.
func (d *Desk) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/admin",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   d.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// dropSession гасит печенье.
//
// Отрицательный MaxAge, а не пустое значение: пустое печенье браузер хранит
// дальше и шлёт обратно, и выход выглядел бы состоявшимся ровно до первой
// перезагрузки. Прочие поля повторяют выданные намеренно — браузер узнаёт
// печенье по имени, пути и домену, и печенье с другим путём он гасит не то.
func (d *Desk) dropSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   d.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// presented достаёт предъявленный токен: сперва из заголовка, потом из
// печенья.
//
// Заголовок первым, потому что он назван вслух: обращение, пришедшее с
// Authorization, предъявляет токен намеренно, а печенье браузер шлёт сам.
// Пустой или негодный заголовок печенья не отменяет — иначе одна кривая
// строка Authorization выбрасывала бы из студии человека с живой сессией.
func presented(r *http.Request) string {
	if head := r.Header.Get("Authorization"); head != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(head, prefix) {
			if token := strings.TrimSpace(head[len(prefix):]); token != "" {
				return token
			}
		}
	}
	if jar, err := r.Cookie(sessionCookie); err == nil {
		return strings.TrimSpace(jar.Value)
	}
	return ""
}
