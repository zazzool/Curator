package totp

import (
	"testing"
	"time"
)

// Секрет из RFC 6238: двадцать знаков «12345678901234567890» в base32.
// Проверка по чужим образцам, а не по своим: своя реализация, сверенная
// сама с собой, сойдётся с собой и разойдётся с телефоном врача.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestCodeСходитсяСОбразцамиRFC(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		got, err := Code(rfcSecret, time.Unix(c.unix, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("на %d код %q, по RFC ожидается %q", c.unix, got, c.want)
		}
	}
}

func TestVerifyДопускаетШагВОбеСтороны(t *testing.T) {
	// Часы телефона и сервера расходятся на секунды, и код, набранный на
	// 29-й секунде, приходит на 31-й.
	now := time.Unix(1111111111, 0)
	prev, _ := Code(rfcSecret, now.Add(-Step))
	next, _ := Code(rfcSecret, now.Add(Step))

	if !Verify(rfcSecret, prev, now) {
		t.Error("код предыдущего шага отвергнут")
	}
	if !Verify(rfcSecret, next, now) {
		t.Error("код следующего шага отвергнут")
	}
}

func TestVerifyНеПускаетСтарыйКод(t *testing.T) {
	now := time.Unix(1111111111, 0)
	old, _ := Code(rfcSecret, now.Add(-5*Step))
	if Verify(rfcSecret, old, now) {
		t.Error("код пятишаговой давности принят: окно шире объявленного")
	}
}

func TestVerifyОтвергаетМусор(t *testing.T) {
	now := time.Unix(1111111111, 0)
	for _, code := range []string{"", "12345", "1234567", "абвгде", "000000"} {
		if Verify(rfcSecret, code, now) && code != "000000" {
			t.Errorf("принят негодный код %q", code)
		}
	}
}

func TestСекретСПробеламиЧитается(t *testing.T) {
	// Аутентификаторы показывают секрет группами по четыре знака, и
	// человек копирует его вместе с пробелами.
	spaced := "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ"
	got, err := Code(spaced, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Errorf("код %q: секрет с пробелами прочитан неверно", got)
	}
}

func TestПустойСекретОтказывает(t *testing.T) {
	if _, err := Code("", time.Now()); err == nil {
		t.Fatal("пустой секрет принят")
	}
}

func TestURIСодержитВсёНужноеДляПривязки(t *testing.T) {
	uri := URI("Куратор", "editor", "gezdgnbv")
	for _, part := range []string{"otpauth://totp/", "Куратор", "editor", "GEZDGNBV", "digits=6", "period=30"} {
		if !contains(uri, part) {
			t.Errorf("в ссылке привязки нет %q: %s", part, uri)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
