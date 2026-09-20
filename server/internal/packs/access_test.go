package packs

import "testing"

func TestОткрытостьНабораРешаетсяЛинейкойИПравами(t *testing.T) {
	// Таблицей, а не десятком проверок: важен не каждый случай сам по
	// себе, а то, что их ровно столько и что ни один не открыт по
	// недосмотру.
	for _, случай := range []struct {
		имя    string
		line   string
		rights Rights
		открыт bool
	}{
		{"гостевой открыт никому не известному", LineGuest, Rights{}, true},
		{"спонсорский открыт никому не известному", LineSponsored, Rights{}, true},
		{"базовый закрыт не назвавшемуся", LineBasic, Rights{}, false},
		{"базовый открыт привязавшему почту", LineBasic, Rights{EmailBound: true}, true},
		{"платный закрыт привязавшему почту", LinePaid, Rights{EmailBound: true}, false},
		{"платный открыт подписчику", LinePaid, Rights{Subscribed: true}, true},
		{"платный открыт купившему", LinePaid, Rights{Owns: true}, true},
		// Купленное сильнее линейки: отобрать оплаченное сменой линейки
		// значило бы отобрать его молча.
		{"купленное переживает смену линейки", LinePaid, Rights{Owns: true}, true},
		// Непонятое не применяется. Линейка с опечаткой — это не «какая-то
		// линейка», а неизвестно что, и открывать по ней нельзя.
		{"незнакомая линейка закрыта", "besplatno", Rights{Subscribed: true}, false},
		{"пустая линейка закрыта", "", Rights{EmailBound: true}, false},
	} {
		if got := OpenTo(случай.line, случай.rights); got != случай.открыт {
			t.Errorf("%s: OpenTo(%q, %+v) = %v", случай.имя, случай.line, случай.rights, got)
		}
	}
}

func TestПереченьЛинеекЗакрыт(t *testing.T) {
	// Линейка, добавленная «для гибкости», не значит ничего ни для
	// OpenTo, ни для витрины: она просто закрыта. Перечень и проверка
	// линейки ходят парой, и разойдись они — студия предложила бы завести
	// набор, который никому не откроется.
	for _, line := range Lines {
		if !KnownLine(line) {
			t.Errorf("линейка %q есть в перечне и не признана известной", line)
		}
		if !OpenTo(line, Rights{Owns: true, Subscribed: true, EmailBound: true}) {
			t.Errorf("линейка %q не открывается никакими правами вообще", line)
		}
	}
	if KnownLine("guest ") {
		t.Error("линейка с пробелом признана известной")
	}
}
