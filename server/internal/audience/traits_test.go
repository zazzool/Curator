package audience

import (
	"encoding/json"
	"testing"
)

// портрет собирает портрет с заполненными картами: пустая карта и
// отсутствие карты для признаков не одно и то же только на письме, а в
// Go отсутствующий ключ даёт ноль — и опыт, забывший карту, был бы
// зелёным по случайности.
func портрет(изменить func(*Portrait)) Portrait {
	p := Portrait{
		Solved:     map[string]int{},
		Correct:    map[string]int{},
		ActiveDays: map[string]int{},
	}
	изменить(&p)
	return p
}

func TestПризнакиСчитаютсяПоПортрету(t *testing.T) {
	for _, случай := range []struct {
		имя      string
		правило  Rule
		портрет  Portrait
		попадает bool
	}{
		{
			"пустое правило подходит всякому",
			Rule{}, портрет(func(*Portrait) {}), true,
		},
		{
			"почта привязана",
			Rule{{Trait: TraitEmail}},
			портрет(func(p *Portrait) { p.EmailBound = true }), true,
		},
		{
			"почта не привязана — отрицанием, а не вторым признаком",
			Rule{{Trait: TraitEmail, Not: true}},
			портрет(func(*Portrait) {}), true,
		},
		{
			"ушедший плательщик: платил, но не подписан",
			Rule{{Trait: TraitPaid}, {Trait: TraitSubscribed, Not: true}},
			портрет(func(p *Portrait) { p.EverPaid = true }), true,
		},
		{
			"действующий подписчик в ушедшие не попадает",
			Rule{{Trait: TraitPaid}, {Trait: TraitSubscribed, Not: true}},
			портрет(func(p *Portrait) { p.EverPaid = true; p.Subscribed = true }), false,
		},
		{
			"признаки соединены «и», а не «или»",
			Rule{{Trait: TraitEmail}, {Trait: TraitSubscribed}},
			портрет(func(p *Portrait) { p.EmailBound = true }), false,
		},
		{
			"стаж не меньше тридцати суток",
			Rule{{Trait: TraitAge, N: 30}},
			портрет(func(p *Portrait) { p.AgeDays = 30 }), true,
		},
		{
			"новичок стажем не вышел",
			Rule{{Trait: TraitAge, N: 30}},
			портрет(func(p *Portrait) { p.AgeDays = 29 }), false,
		},
		{
			"новички — отрицанием стажа",
			Rule{{Trait: TraitAge, N: 7, Not: true}},
			портрет(func(p *Portrait) { p.AgeDays = 2 }), true,
		},
		{
			"уснувшие: не заходил девяносто суток",
			Rule{{Trait: TraitIdle, N: 90}},
			портрет(func(p *Portrait) { p.IdleDays = 120 }), true,
		},
		{
			"решил не меньше сотни за всё время",
			Rule{{Trait: TraitSolved, N: 100}},
			портрет(func(p *Portrait) { p.Solved[WindowAll] = 100 }), true,
		},
		{
			"окно считается своё, а не любое",
			Rule{{Trait: TraitSolved, N: 100, Over: Window7}},
			портрет(func(p *Portrait) { p.Solved[WindowAll] = 500 }), false,
		},
		{
			"доля верных не ниже восьмидесяти",
			Rule{{Trait: TraitAccuracy, N: 80}},
			портрет(func(p *Portrait) { p.Solved[WindowAll] = 10; p.Correct[WindowAll] = 8 }), true,
		},
		{
			"доля верных ниже порога",
			Rule{{Trait: TraitAccuracy, N: 80}},
			портрет(func(p *Portrait) { p.Solved[WindowAll] = 10; p.Correct[WindowAll] = 7 }), false,
		},
		{
			"не решивший ни одной задачи в отличники не попадает",
			Rule{{Trait: TraitAccuracy, N: 80}},
			портрет(func(*Portrait) {}), false,
		},
		{
			"порог попыток ставится вторым признаком того же правила",
			Rule{{Trait: TraitAccuracy, N: 80}, {Trait: TraitSolved, N: 50}},
			портрет(func(p *Portrait) { p.Solved[WindowAll] = 10; p.Correct[WindowAll] = 10 }), false,
		},
		{
			"занимался не меньше двадцати дней за девяносто суток",
			Rule{{Trait: TraitActiveDays, N: 20, Over: Window90}},
			портрет(func(p *Portrait) { p.ActiveDays[Window90] = 21 }), true,
		},
		{
			"решивший много за один день прилежным не считается",
			Rule{{Trait: TraitActiveDays, N: 20, Over: Window30}},
			портрет(func(p *Portrait) { p.Solved[Window30] = 900; p.ActiveDays[Window30] = 1 }), false,
		},
	} {
		if got := случай.правило.Matches(случай.портрет); got != случай.попадает {
			t.Errorf("%s: попадает = %v, ждали %v", случай.имя, got, случай.попадает)
		}
	}
}

func TestНепонятыйПризнакНеВыполняется(t *testing.T) {
	// Сюда не попасть через ParseRule, и это единственная защита от
	// правила, добравшегося мимо него — например, записанного прежней
	// редакцией сервера. Открывать по непонятому признаку нельзя, и
	// закрывать по нему тоже: непонятое не применяется, а группа с таким
	// правилом целиком отбрасывается при чтении (см. live).
	rule := Rule{{Trait: "nravitsya-vrachu"}}
	if rule.Matches(портрет(func(p *Portrait) { p.EmailBound = true })) {
		t.Error("правило с непонятым признаком подошло")
	}
	// И отрицание его не делает истиной автоматически: «не выполняется»
	// у непонятого признака означало бы «выполняется» у его отрицания, и
	// тогда опечатка в признаке раздавала бы наборы всем подряд.
	if err := rule.Check(); err == nil {
		t.Error("непонятый признак прошёл проверку правила")
	}
}

func TestПравилоОтказываетНаНегодном(t *testing.T) {
	for _, случай := range []struct {
		имя  string
		rule Rule
	}{
		{"признака не бывает", Rule{{Trait: "vip"}}},
		{"у флага нет порога", Rule{{Trait: TraitEmail, N: 5}}},
		{"у флага нет окна", Rule{{Trait: TraitSubscribed, Over: Window7}}},
		{"порог отрицателен", Rule{{Trait: TraitSolved, N: -1}}},
		{"доля выше ста процентов", Rule{{Trait: TraitAccuracy, N: 101}}},
		{"окна не бывает", Rule{{Trait: TraitSolved, N: 1, Over: "2d"}}},
		{"у стажа нет окна", Rule{{Trait: TraitAge, N: 30, Over: Window30}}},
	} {
		if err := случай.rule.Check(); err == nil {
			t.Errorf("%s: правило принято", случай.имя)
		}
	}
}

func TestПравилоРазбираетсяИзБазы(t *testing.T) {
	// Правило лежит в базе одним JSONB, и проверка эта — про то, что
	// записанное нами читается нами же. Разойдись имена полей — группа
	// молча стала бы пустой, а не сломанной.
	rule := Rule{
		{Trait: TraitPaid},
		{Trait: TraitSolved, N: 100, Over: Window30},
		{Trait: TraitSubscribed, Not: true},
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("правило не записано: %v", err)
	}
	back, err := ParseRule(raw)
	if err != nil {
		t.Fatalf("правило не прочитано: %v", err)
	}
	if len(back) != len(rule) {
		t.Fatalf("прочитано %d признаков, записано %d", len(back), len(rule))
	}
	for i := range rule {
		if back[i] != rule[i] {
			t.Errorf("признак %d: прочитано %+v, записано %+v", i, back[i], rule[i])
		}
	}

	// Непонятое не применяется: правило с негодным признаком не
	// разбирается наполовину.
	if _, err := ParseRule([]byte(`[{"trait":"vip"}]`)); err == nil {
		t.Error("правило с негодным признаком разобралось")
	}
	if _, err := ParseRule([]byte(`{"trait":"email-bound"}`)); err == nil {
		t.Error("правило, записанное не списком, разобралось")
	}
}

func TestПоведениеСпрашиваетсяТолькоКогдаНужно(t *testing.T) {
	// Поведенческая половина портрета читает попытки врача, а их у
	// давнего пользователя десятки тысяч, и портрет считается на каждом
	// обращении за корпусом. Пока ни одно правило про поведение не
	// спрашивает, этот запрос не должен идти вовсе.
	if (Rule{{Trait: TraitEmail}, {Trait: TraitPaid}}).NeedsBehaviour() {
		t.Error("правило без поведенческих признаков просит читать попытки")
	}
	if !(Rule{{Trait: TraitEmail}, {Trait: TraitSolved, N: 1}}).NeedsBehaviour() {
		t.Error("правило с поведенческим признаком не просит читать попытки")
	}
}

func TestСкрытиеСильнееОткрытия(t *testing.T) {
	// Пересечение групп неизбежно: врач бывает и в «кафедре», и в
	// «уснувших». Выигрывать при споре должна скрывающая — закрытое по
	// ошибке виден сразу, открытое по ошибке не видит никто.
	const кафедра, уснувшие, чужая = int64(1), int64(2), int64(3)
	bindings := []Binding{
		{PackID: 10, AudienceID: кафедра, Mode: ModeOpen},
		{PackID: 10, AudienceID: уснувшие, Mode: ModeHidden},
		{PackID: 20, AudienceID: чужая, Mode: ModeOpen},
		{PackID: 30, AudienceID: кафедра, Mode: ModeOpen},
	}

	v := Verdicts([]int64{кафедра, уснувшие}, bindings)
	if !v[10].Hidden || !v[10].Granted {
		t.Fatalf("набор 10: ждали оба довода, получили %+v", v[10])
	}
	if v[20].Granted {
		t.Error("набор 20 открыт по группе, в которой врача нет")
	}
	if !v[30].Granted {
		t.Error("набор 30 не открыт по группе, в которой врач состоит")
	}

	// Врач без групп не получает ни одного довода: до групп корпус у
	// него был таким же, и он обязан таким остаться.
	if len(Verdicts(nil, bindings)) != 0 {
		t.Error("врачу без групп достались доводы")
	}

	// Непонятая связь не применяется: молчаливое «считаем открывающей»
	// раздало бы набор по строке, которой никто не писал.
	странная := Verdicts([]int64{кафедра}, []Binding{{PackID: 40, AudienceID: кафедра, Mode: "maybe"}})
	if странная[40].Granted || странная[40].Hidden {
		t.Errorf("непонятая связь применилась: %+v", странная[40])
	}
}
