package backup

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func ключ(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("ключ не порождён: %v", err)
	}
	return key
}

// Запечатанное распечатывается обратно байт в байт.
//
// Размеры нарочно вокруг границы куска: ровно кусок, кусок без байта,
// кусок с байтом и несколько кусков. Ошибка в работе по кускам сидит
// именно на границе, и на одном размере не показывается.
func TestЗапечатанноеРаспечатываетсяБайтВБайт(t *testing.T) {
	key := ключ(t)
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 7} {
		plain := make([]byte, size)
		if _, err := rand.Read(plain); err != nil {
			t.Fatalf("данные не порождены: %v", err)
		}

		var sealed bytes.Buffer
		if err := Seal(&sealed, bytes.NewReader(plain), key); err != nil {
			t.Fatalf("%d байт не запечаталось: %v", size, err)
		}
		var back bytes.Buffer
		if err := Open(&back, bytes.NewReader(sealed.Bytes()), key); err != nil {
			t.Fatalf("%d байт не распечаталось: %v", size, err)
		}
		if !bytes.Equal(back.Bytes(), plain) {
			t.Fatalf("на %d байтах распечаталось не то, что запечатывали", size)
		}
	}
}

// Запечатанное не читается без ключа.
//
// Ради этого всё и написано: снимок обязан уезжать нечитаемым для того,
// кто его хранит.
func TestЗапечатанноеНеЧитаетсяГлазами(t *testing.T) {
	key := ключ(t)
	plain := []byte(strings.Repeat("Инфаркт миокарда, диагноз врача. ", 100))

	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader(plain), key); err != nil {
		t.Fatalf("не запечаталось: %v", err)
	}
	if bytes.Contains(sealed.Bytes(), []byte("Инфаркт")) {
		t.Fatal("текст снимка виден в запечатанном файле")
	}
}

// Чужой ключ не подходит.
func TestЧужойКлючНеПодходит(t *testing.T) {
	var sealed bytes.Buffer
	if err := Seal(&sealed, strings.NewReader("тайна"), ключ(t)); err != nil {
		t.Fatalf("не запечаталось: %v", err)
	}
	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(sealed.Bytes()), ключ(t)); err == nil {
		t.Fatal("чужим ключом распечаталось")
	}
	if back.Len() > 0 {
		t.Fatal("чужим ключом что-то вытекло наружу")
	}
}

// Подменённый байт ловится печатью.
func TestПодменённыйБайтЛовится(t *testing.T) {
	key := ключ(t)
	var sealed bytes.Buffer
	if err := Seal(&sealed, strings.NewReader(strings.Repeat("я", 1000)), key); err != nil {
		t.Fatalf("не запечаталось: %v", err)
	}
	raw := sealed.Bytes()
	raw[len(raw)-1] ^= 0xFF

	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(raw), key); err == nil {
		t.Fatal("подменённый байт прошёл")
	}
}

// Обрезанный файл не выдаёт себя за целый.
//
// Самый опасный случай из всех: оборванная закачка даёт файл, который
// выглядит снимком, и негодность его выясняется в тот единственный день,
// когда снимок нужен.
func TestОбрезанныйФайлНеВыдаётСебяЗаЦелый(t *testing.T) {
	key := ключ(t)
	plain := make([]byte, 3*chunkSize)
	if _, err := rand.Read(plain); err != nil {
		t.Fatalf("данные не порождены: %v", err)
	}
	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader(plain), key); err != nil {
		t.Fatalf("не запечаталось: %v", err)
	}

	// Ровно два куска из четырёх: обрез по границе, а не посередине.
	// Посередине заметил бы и простой счётчик длины.
	обрез := len(sealMagic) + 2*(4+chunkSize+16)
	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(sealed.Bytes()[:обрез]), key); err == nil {
		t.Fatal("обрезанный ровно по границе куска файл распечатался как целый")
	}
}

// Переставленные куски не складываются в годный файл.
//
// Без порядкового номера в печати «запечатано по кускам» означало бы, что
// любой набор наших же кусков сходится.
func TestПереставленныеКускиНеСходятся(t *testing.T) {
	key := ключ(t)
	plain := make([]byte, 3*chunkSize)
	if _, err := rand.Read(plain); err != nil {
		t.Fatalf("данные не порождены: %v", err)
	}
	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader(plain), key); err != nil {
		t.Fatalf("не запечаталось: %v", err)
	}

	raw := sealed.Bytes()
	шаг := 4 + chunkSize + 16
	первый := len(sealMagic)
	второй := первый + шаг

	переставленный := make([]byte, 0, len(raw))
	переставленный = append(переставленный, raw[:первый]...)
	переставленный = append(переставленный, raw[второй:второй+шаг]...)
	переставленный = append(переставленный, raw[первый:первый+шаг]...)
	переставленный = append(переставленный, raw[второй+шаг:]...)

	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(переставленный), key); err == nil {
		t.Fatal("переставленные куски сложились в годный файл")
	}
}

// Ключ не той длины — отказ, а не дополнение нулями.
//
// «Дополним нулями» превращает опечатку в переменной окружения в
// шифрование ключом из одних нулей, и происходит это молча.
func TestКлючНеТойДлиныОтказывает(t *testing.T) {
	for _, length := range []int{0, 16, 31, 33} {
		var sealed bytes.Buffer
		if err := Seal(&sealed, strings.NewReader("тайна"), make([]byte, length)); err == nil {
			t.Fatalf("ключ в %d байт подошёл", length)
		}
	}
}

// Чужой файл не принимается за запечатанный снимок.
func TestЧужойФайлНеПринимаетсяЗаСнимок(t *testing.T) {
	var back bytes.Buffer
	err := Open(&back, strings.NewReader("PGDMP какой-то обычный дамп"), ключ(t))
	if err == nil {
		t.Fatal("обычный файл распечатался")
	}
	if !strings.Contains(err.Error(), "метка") {
		t.Fatalf("отказ ничего не объясняет: %v", err)
	}
}
