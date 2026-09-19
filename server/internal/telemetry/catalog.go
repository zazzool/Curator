// Телеметрия: что врач делал, кроме решения задач.
//
// # Словарь закрыт, и это главное в разделе
//
// Событие, которого нет в словаре, отбрасывается, а не записывается «на
// всякий случай». Открытый словарь превращает телеметрию в свалку, по
// которой нельзя построить ни одного отчёта: имена расходятся по сборкам,
// одно и то же зовётся двумя словами, и понять, какое из них считать,
// нельзя уже никому.
//
// # Попытки сюда не едут
//
// Что врач ответил и верно ли — это попытка, и у неё своя дверь
// (`POST /v1/attempts`): из попыток считается прогресс, повторение и
// решаемость. Телеметрия — про всё остальное: открыл разбор или ушёл,
// докачал набор или бросил, дошёл ли от витрины до покупки. Запиши мы ответ
// обоими путями — и два счёта решённых задач разошлись бы молча.
package telemetry

// Event — событие словаря.
type Event struct {
	Name string

	// Title — как событие называется в отчёте. По-русски: отчёт читает
	// человек, а не тот, кто писал приложение.
	Title string

	// Props — разрешённые свойства. Словарь свойств закрыт так же, как
	// словарь имён: свойство, приехавшее по месту, не попадёт ни в один
	// отчёт, но останется в базе и будет выглядеть данными.
	Props []string
}

// Catalog — словарь целиком.
//
// Порядок значим: он же порядок разделов в отчётах, и в нём читается путь
// врача — от запуска к разбору, от разбора к набору, от набора к покупке.
func Catalog() []Event {
	return []Event{
		// Запуск и заход.
		{Name: "app_opened", Title: "Запуск приложения", Props: []string{"cold"}},
		{Name: "session_ended", Title: "Конец захода", Props: []string{"spentMs", "cases"}},

		// Разбор задач. Ответ сюда не едет — он попытка.
		{Name: "case_shown", Title: "Задача показана", Props: []string{"caseId", "mode"}},
		{Name: "case_skipped", Title: "Задача пропущена", Props: []string{"caseId", "mode"}},
		{Name: "explanation_opened", Title: "Открыт разбор", Props: []string{"caseId"}},
		{Name: "fragment_opened", Title: "Открыт фрагмент условия", Props: []string{"caseId", "fragment"}},

		// Повторение по интервалам.
		{Name: "review_started", Title: "Начато повторение", Props: []string{"due"}},
		{Name: "review_finished", Title: "Повторение доведено", Props: []string{"done", "due"}},

		// Наборы: витрина и закачка.
		{Name: "pack_opened", Title: "Открыт набор на витрине", Props: []string{"pack"}},
		{Name: "pack_download_started", Title: "Начата закачка набора", Props: []string{"pack", "version"}},
		{Name: "pack_download_finished", Title: "Набор докачан", Props: []string{"pack", "version", "spentMs"}},
		{Name: "pack_download_failed", Title: "Закачка набора сорвалась", Props: []string{"pack", "version", "reason"}},

		// Покупка. Воронка держится на этих трёх: сколько увидели цену,
		// сколько нажали «купить», сколько дошли.
		{Name: "paywall_shown", Title: "Показана цена закрытого набора", Props: []string{"pack"}},
		{Name: "purchase_started", Title: "Нажата покупка", Props: []string{"purpose"}},
		{Name: "purchase_finished", Title: "Покупка состоялась", Props: []string{"purpose"}},

		// Награды.
		{Name: "sign_seen", Title: "Врач увидел знак", Props: []string{"sign"}},
		{Name: "level_seen", Title: "Врач увидел новый уровень", Props: []string{"level"}},
	}
}

// Known говорит, есть ли такое событие в словаре, и какое.
func Known(name string) (Event, bool) {
	for _, one := range Catalog() {
		if one.Name == name {
			return one, true
		}
	}
	return Event{}, false
}

// Allowed отвечает, разрешено ли свойство у этого события.
func (e Event) Allowed(prop string) bool {
	for _, known := range e.Props {
		if known == prop {
			return true
		}
	}
	return false
}
