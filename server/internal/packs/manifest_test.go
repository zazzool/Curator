package packs

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Сверка с общим эталоном подписи.
//
// Подпись сходится лишь тогда, когда обе стороны приводят манифест к
// байтам одинаково. Разойдись два способа — и подпись не сойдётся ни на
// одном устройстве, причём глазами оба текста будут выглядеть одинаково.

type reference struct {
	Example struct {
		Manifest struct {
			Pack       string `json:"pack"`
			Version    int64  `json:"version"`
			Title      string `json:"title"`
			ReleasedAt string `json:"releasedAt"`
			Cases      []struct {
				ID   string `json:"id"`
				Ord  int    `json:"ord"`
				Hash string `json:"hash"`
			} `json:"cases"`
		} `json:"manifest"`
		Canonical string `json:"canonical"`
		PublicKey string `json:"publicKey"`
		Signature string `json:"signature"`
	} `json:"example"`

	BodyHash struct {
		Example struct {
			Body      string `json:"body"`
			Canonical string `json:"canonical"`
			Hash      string `json:"hash"`
		} `json:"example"`
	} `json:"bodyHash"`
}

func loadReference(t *testing.T) reference {
	t.Helper()
	path := filepath.Join("..", "..", "..", "shared", "pack-manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("эталон подписи %s не прочитан: %v. "+
			"Это отказ, а не пропуск: без него подпись сервера и сверка в "+
			"приложении расходятся молча", path, err)
	}
	var out reference
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("эталон подписи не разобран: %v", err)
	}
	if out.Example.Canonical == "" || out.Example.Signature == "" {
		t.Fatal("в эталоне нет разобранного примера: сверять нечего")
	}
	return out
}

func fromReference(t *testing.T, r reference) Manifest {
	t.Helper()
	at, err := time.Parse(time.RFC3339, r.Example.Manifest.ReleasedAt)
	if err != nil {
		t.Fatalf("время выпуска в эталоне не разобрано: %v", err)
	}
	m := Manifest{
		Pack: r.Example.Manifest.Pack, Version: r.Example.Manifest.Version,
		Title: r.Example.Manifest.Title, ReleasedAt: at,
	}
	for _, one := range r.Example.Manifest.Cases {
		m.Cases = append(m.Cases, Item{ID: one.ID, Ord: one.Ord, Hash: one.Hash})
	}
	return m
}

func TestКаноническийВидСходитсяСЭталономБайтВБайт(t *testing.T) {
	r := loadReference(t)
	got, err := Canonical(fromReference(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != r.Example.Canonical {
		t.Errorf("канонический вид разошёлся с эталоном\nполучено: %s\nэталон:   %s",
			got, r.Example.Canonical)
	}
}

func TestАмперсандВНазванииНеЭкранируется(t *testing.T) {
	// Библиотека Go по умолчанию превращает <, > и & в & и прочее.
	// Пакет с амперсандом в названии подписался бы одними байтами, а на
	// устройстве свернулся бы в другие — и подпись не сошлась бы ровно у
	// того выпуска, у которого в названии стоит «и».
	m := Manifest{Pack: "p", Version: 1, Title: "а & б <в> \"г\"",
		ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	got, err := Canonical(m)
	if err != nil {
		t.Fatal(err)
	}
	// Ищется именно экранированная запись — обратная косая и u0026, а не
	// сам символ: сам символ в каноническом виде как раз и должен быть.
	for _, bad := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if contains(string(got), bad) {
			t.Errorf("в канонический вид попало экранирование %s: %s", bad, got)
		}
	}
}

func TestКаноническийВидНеКончаетсяПереводомСтроки(t *testing.T) {
	// Лишний байт с одной стороны и его отсутствие с другой — то же
	// расхождение, что и порядок ключей, только незаметнее.
	got, err := Canonical(Manifest{Pack: "p", Version: 1,
		ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1] != '}' {
		t.Errorf("канонический вид кончается не скобкой: %q", got)
	}
}

func TestПодписьИзЭталонаСходится(t *testing.T) {
	r := loadReference(t)
	key, err := ParsePublicKey(r.Example.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(fromReference(t, r), r.Example.Signature, key); err != nil {
		t.Errorf("подпись из эталона не сошлась: %v", err)
	}
}

func TestПодменаСоставаЛомаетПодпись(t *testing.T) {
	// Ради этого всё и написано: между устройством и сервером стоит чужая
	// сеть, и подменить состав по дороге может всякий, кто в ней сидит.
	r := loadReference(t)
	key, err := ParsePublicKey(r.Example.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	m := fromReference(t, r)
	m.Cases[0].Hash = "sha256:подменили"
	if err := Verify(m, r.Example.Signature, key); err == nil {
		t.Error("подменённый состав прошёл сверку подписи")
	}
}

func TestЧужойКлючНеПодходит(t *testing.T) {
	r := loadReference(t)
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(fromReference(t, r), r.Example.Signature, other); err == nil {
		t.Error("подпись сошлась с чужим ключом")
	}
}

func TestПодписьПустымКлючомОтказывает(t *testing.T) {
	// Подпись, которую никто не может проверить, выглядит на устройстве
	// точно так же, как настоящая, — до первой сверки.
	if _, err := Sign(Manifest{Pack: "p"}, nil); err == nil {
		t.Error("манифест подписался пустым ключом")
	}
}

func TestКругПодписьСверкаСходится(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{Pack: "свежий", Version: 7, Title: "Заголовок",
		ReleasedAt: time.Now(),
		Cases:      []Item{{ID: "c-1", Ord: 0, Hash: "sha256:aa"}}}
	sig, err := Sign(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(m, sig, pub); err != nil {
		t.Errorf("своя же подпись не сошлась: %v", err)
	}
}

func TestОтпечатокСодержанияСходитсяСЭталоном(t *testing.T) {
	// Отпечаток берётся от канонического вида, а не от байтов из базы:
	// JSONB не хранит ни порядок ключей, ни пробелы.
	r := loadReference(t)
	got, err := HashBody([]byte(r.BodyHash.Example.Body))
	if err != nil {
		t.Fatal(err)
	}
	if got != r.BodyHash.Example.Hash {
		t.Errorf("отпечаток %s, эталон обещает %s", got, r.BodyHash.Example.Hash)
	}
}

func TestОтпечатокНеЗависитОтПорядкаКлючей(t *testing.T) {
	first, err := HashBody([]byte(`{"a":1,"b":{"x":1,"y":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashBody([]byte(`{"b":{"y":2,"x":1},  "a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("тот же состав дал разные отпечатки: %s и %s", first, second)
	}
}

// Ниже — мелочи, которых в разобранном примере эталона нет, а стандартные
// сборщики JSON на них расходятся: целое, приехавшее дробью, управляющие
// символы, U+2028. Каждое такое расхождение неотличимо от подмены —
// подпись просто не сходится, и искать причину будут неделю. Встречные
// проверки с той стороны — в app/test/packs_canonical_test.dart; снимать
// можно только парой.

func TestЗаписьУправляющихСимволовПришпилена(t *testing.T) {
	// Не «длинной записью» и не «короткой»: и так, и так. Догадка, что Go
	// пишет управляющие только длинной записью, уже была и оказалась
	// неверной — поэтому здесь замер, а не рассуждение. Сторона Dart
	// обязана повторить ровно это (app/test/packs_canonical_test.dart).
	got, err := Canonical(Manifest{Pack: "p", Version: 1,
		Title:      "\b\f\n\r\t",
		ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(got), `"`+BS+`b`+BS+`f`+BS+`n`+BS+`r`+BS+`t"`) {
		t.Errorf("короткая запись разошлась с замером: %s", got)
	}
	got, err = Canonical(Manifest{Pack: "p", Version: 1,
		Title:      "\u0001\u000b\u001f",
		ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(got), `"`+BS+`u0001`+BS+`u000b`+BS+`u001f"`) {
		t.Errorf("длинная запись разошлась с замером: %s", got)
	}
}

func TestРазделителиСтрокЭкранируютсяВсегда(t *testing.T) {
	// U+2028 и U+2029 Go экранирует даже с выключенным экранированием
	// HTML: в JavaScript они рвут строку, и отменить это нечем. Сторона
	// Dart их не экранирует сама — значит, обязана повторить. Это
	// единственное место, где сборщики расходятся по-настоящему.
	got, err := Canonical(Manifest{Pack: "p", Version: 1,
		Title:      "\u2028\u2029",
		ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(got), `"`+BS+`u2028`+BS+`u2029"`) {
		t.Errorf("разделители строк не экранированы: %s", got)
	}
}

func TestЦелоеПриехавшееДробьюПишетсяЦелым(t *testing.T) {
	// json.Unmarshal кладёт всякое число в float64, и Marshal пишет
	// float64(1) как «1». Библиотека Dart на том же месте пишет «1.0», и
	// отпечаток не сошёлся бы на ровном месте.
	first, err := HashBody([]byte(`{"n":1.0}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashBody([]byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("1.0 и 1 дали разные отпечатки: %s и %s", first, second)
	}
}

func TestИспорченноеСодержаниеНеПревращаетсяВОтпечаток(t *testing.T) {
	// Непонятое не применяется: отпечаток от неразобранного выглядел бы
	// настоящим и не сошёлся бы ни с чем.
	if _, err := HashBody([]byte("совсем не json")); err == nil {
		t.Error("неразобранное содержание получило отпечаток")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// BS — обратная косая. Собрана отдельной постоянной, а не вписана в
// каждую ожидаемую строку: сам знак в тексте проверки легко подменяется
// самим экранируемым символом при переносе текста, и проверка тогда
// начинает сходиться сама с собой, ничего не проверяя. Так здесь уже
// было — со стороны Dart, и поймалось случайно.
const BS = string(rune(92))
