// Вход студии: пользователи, их права и сессии.
//
// Права проверяются на сервере, а не скрытием разделов в студии. Скрытая
// вкладка при открытой ручке — это подсказка, где искать, а не запрет:
// человек, открывший инструменты разработчика, увидит и адрес, и ответ.
package studio

import "fmt"

// Permission — право. Словарь закрыт: право, появившееся строкой в коде
// маршрута, не попадёт ни в один список и не будет выдано ни одному
// пользователю — то есть маршрут окажется закрыт для всех, и заметят это
// не сразу.
type Permission string

const (
	// Источники: читать и принимать разбор — разные права. Принять разбор
	// значит решить, что теперь считается истиной источника, и давать это
	// всякому, кто может посмотреть, незачем.
	PermSourceRead   Permission = "source:read"
	PermSourceAccept Permission = "source:accept"

	// Задачи: читать и править.
	PermCaseRead  Permission = "case:read"
	PermCaseWrite Permission = "case:write"

	// Генерация: запускать конвейер и править промпты.
	PermGenerate Permission = "generate"
	PermPrompts  Permission = "prompts"

	// Наборы задач: собрать набор и выпустить его. Выпуск — отдельное
	// право, потому что выпустить значит подписать нашим ключом то, что
	// встанет на устройства и переживёт любую правку: выпуск неизменяем.
	PermPacks Permission = "packs"

	// Клиенты, продажи и отчёты.
	PermClients   Permission = "clients"
	PermSales     Permission = "sales"
	PermAnalytics Permission = "analytics"

	// Мастерская: пользователи студии, ключи программ, состояние службы.
	PermWorkshop Permission = "workshop"
)

// All — все права. Порядок значим: он задаёт порядок в студии, и менять его
// без нужды не надо — человек ищет право глазами по привычному месту.
var All = []Permission{
	PermSourceRead, PermSourceAccept,
	PermCaseRead, PermCaseWrite,
	PermGenerate, PermPrompts,
	PermPacks,
	PermClients, PermSales, PermAnalytics,
	PermWorkshop,
}

// Known говорит, есть ли такое право в словаре.
func Known(p Permission) bool {
	for _, known := range All {
		if known == p {
			return true
		}
	}
	return false
}

// ParsePermissions разбирает права из строк, отказывая на незнакомом.
//
// Отказ, а не пропуск: право с опечаткой, молча выброшенное при заведении
// пользователя, оставит человека без доступа к разделу, и искать причину
// будут в маршруте, а не в опечатке.
func ParsePermissions(raw []string) ([]Permission, error) {
	out := make([]Permission, 0, len(raw))
	for _, r := range raw {
		p := Permission(r)
		if !Known(p) {
			return nil, fmt.Errorf("права %q не существует", r)
		}
		out = append(out, p)
	}
	return out, nil
}
