package limits

import "net/http"

// Заголовки безопасности для браузера.
//
// # Почему они здесь, а не у прокси
//
// Ни одного такого заголовка не было: ни Content-Security-Policy, ни
// X-Frame-Options, ни X-Content-Type-Options. Отдано это было «общему
// nginx на хосте» — настройке, которой нет в репозитории. То есть она не
// проверяется ничем, не переживает переезд контура и не существует вовсе
// на машине того, кто поднял службу у себя.
//
// Цена вопроса конкретна: студия оформляет возврат денег в один щелчок.
// Без X-Frame-Options её можно вложить в чужую страницу, прикрыть своей
// кнопкой и получить щелчок оператора по чужой воле.
//
// Прокси при этом никуда не девается и может ставить своё: браузер
// читает заголовок один раз, а два одинаковых ставят одно и то же.

// Headers добавляет заголовки безопасности ко всякому ответу.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()

		// Ни во фрейм, ни в объект: студию нельзя вложить в чужую
		// страницу и накрыть своей кнопкой.
		head.Set("X-Frame-Options", "DENY")

		// Браузер не додумывает тип по содержимому. Принесённый в
		// источник документ, отданный обратно, иначе может быть
		// истолкован как HTML и исполнен как наша страница.
		head.Set("X-Content-Type-Options", "nosniff")

		// Чужому сайту достаётся только имя нашего, без пути: в пути
		// стоят опознаватели задач, наборов и клиентов.
		head.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Ничего постороннего: ни камеры, ни микрофона, ни места. Студии
		// это не нужно, а вложенному в неё чужому — нужно.
		head.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		// Правила загрузки. `default-src 'self'` — всё своё; сборщик
		// студии вставляет стили строкой, поэтому им разрешено
		// 'unsafe-inline', а скриптам НЕ разрешено: скрипт со стороны и
		// есть то, ради чего эта строка пишется.
		//
		// `frame-ancestors 'none'` повторяет X-Frame-Options для
		// браузеров, которые читают только его.
		head.Set("Content-Security-Policy", contentSecurityPolicy)

		next.ServeHTTP(w, r)
	})
}

const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"
