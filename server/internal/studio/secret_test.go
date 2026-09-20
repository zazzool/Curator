package studio

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"curator/server/internal/totp"
)

func ключ(b byte) []byte {
	key := make([]byte, SealKeySize)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestЗапечатанныйСекретНеВиденВБазе(t *testing.T) {
	const plain = "JBSWY3DPEHPK3PXP"
	stored, err := sealSecret(ключ(1), plain)
	if err != nil {
		t.Fatal(err)
	}
	// Весь смысл в этом: унёсший снимок базы уносит нечитаемое.
	if strings.Contains(stored, plain) {
		t.Fatalf("секрет виден в хранимом значении: %q", stored)
	}

	got, sealed, err := openSecret(ключ(1), stored)
	if err != nil {
		t.Fatal(err)
	}
	if !sealed {
		t.Error("запечатанное значение не признано запечатанным")
	}
	if got != plain {
		t.Errorf("распечаталось %q вместо %q", got, plain)
	}
}

func TestБезКлючаСекретЛежитКакЛежал(t *testing.T) {
	// Выкатка на контур, где ключ ещё не положили, не должна закрывать
	// вход в студию всем сразу.
	const plain = "JBSWY3DPEHPK3PXP"
	stored, err := sealSecret(nil, plain)
	if err != nil {
		t.Fatal(err)
	}
	if stored != plain {
		t.Fatalf("без ключа секрет изменился: %q", stored)
	}
	got, sealed, err := openSecret(nil, stored)
	if err != nil {
		t.Fatal(err)
	}
	if sealed {
		t.Error("открытое значение признано запечатанным")
	}
	if got != plain {
		t.Errorf("прочиталось %q вместо %q", got, plain)
	}
}

func TestЗапечатанноеБезКлючаОтказывает(t *testing.T) {
	// Прочитанное как есть, оно стало бы негодным секретом: сверка
	// ответила бы «код не подошёл», человек перепривязал бы
	// аутентификатор и стёр запечатанное. Непонятое не применяется.
	stored, err := sealSecret(ключ(1), "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openSecret(nil, stored); err == nil {
		t.Fatal("запечатанный секрет без ключа прочитался")
	}
}

func TestЧужимКлючомНеРаспечатывается(t *testing.T) {
	stored, err := sealSecret(ключ(1), "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openSecret(ключ(2), stored); err == nil {
		t.Fatal("секрет распечатался чужим ключом")
	}
}

func TestИспорченныйШифртекстОтказывает(t *testing.T) {
	// Печать GCM затем и стоит: правленную строку нельзя принять за
	// целую, и молчаливо принятая пустота здесь означала бы вход.
	stored, err := sealSecret(ключ(1), "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	// Портим БАЙТ, а не знак base64. Подменённый знак в конце строки
	// несёт всего четыре значащих бита, и в одном случае из шестнадцати
	// совпадал с прежним — «испорченное» оставалось целым, и проверка
	// падала раз в шестнадцать прогонов у того, кто её не писал.
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPrefix))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF
	spoiled := sealedPrefix + base64.StdEncoding.EncodeToString(raw)
	if _, _, err := openSecret(ключ(1), spoiled); err == nil {
		t.Fatal("испорченный шифртекст прочитался")
	}
}

func TestКороткийКлючНеДополняется(t *testing.T) {
	// «Дополним нулями» превращает опечатку в переменной окружения в
	// шифрование ключом из одних нулей, и происходит это молча.
	if _, err := sealSecret([]byte("коротко"), "JBSWY3DPEHPK3PXP"); err == nil {
		t.Fatal("короткий ключ принят")
	}
}

func TestДваЗапечатыванияДаютРазныеСтроки(t *testing.T) {
	// Одинаковые строки на одинаковых секретах сказали бы читающему базу,
	// у кого из сотрудников секреты совпадают, — а совпадают они, когда
	// одного завели дважды.
	first, err := sealSecret(ключ(1), "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	second, err := sealSecret(ключ(1), "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("одноразовое число не меняется: две одинаковые строки в базе")
	}
}

func TestPgСекретЛожитсяВБазуЗапечатанным(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, ключ(7))

	login := newLogin()
	_, secret, err := users.Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := gate.QueryRow(ctx,
		`SELECT totp_secret FROM users WHERE login = $1`, login).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == secret {
		t.Fatal("секрет лежит в базе открытым текстом")
	}
	if !strings.HasPrefix(stored, sealedPrefix) {
		t.Fatalf("у хранимого значения нет метки запечатанного: %q", stored)
	}

	// Отданный при заведении секрет — открытый: его показывают человеку
	// один раз, чтобы он привязал аутентификатор.
	_, read, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if read != secret {
		t.Errorf("прочиталось %q вместо выданного %q", read, secret)
	}
}

func TestPgОткрытыйСекретЗапечатываетсяПриПервомВходе(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)

	// Заведён без ключа — как заведены все, кто есть на контуре сегодня.
	login := newLogin()
	_, secret, err := NewUsers(gate, nil).Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Ключ положили. Догонять отдельным накатом нельзя: накат идёт до
	// подмены кода, и старый код перестал бы пускать в студию всех сразу.
	users := NewUsers(gate, ключ(7))
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.VerifyCode(ctx, login, code, time.Now()); err != nil {
		t.Fatalf("вход по годному коду не прошёл: %v", err)
	}

	var stored string
	if err := gate.QueryRow(ctx,
		`SELECT totp_secret FROM users WHERE login = $1`, login).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, sealedPrefix) {
		t.Fatalf("секрет остался открытым после удачного входа: %q", stored)
	}

	// И следующий вход идёт уже по запечатанному. Время порождения кода и
	// время сверки — одно: разойдись они на полминуты, и код окажется из
	// соседнего окна, а отказ будет выглядеть как непрочитанный секрет.
	later := time.Now().Add(time.Minute)
	next, err := totp.Code(secret, later)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.VerifyCode(ctx, login, next, later); err != nil {
		t.Fatalf("вход по запечатанному секрету не прошёл: %v", err)
	}
}

func TestPgЗапечатанныйСекретБезКлючаНеПускает(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)

	login := newLogin()
	_, secret, err := NewUsers(gate, ключ(7)).Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Ключ потеряли. Пустить по прочитанному как есть значило бы сказать
	// «код не подошёл» и отправить человека перепривязывать
	// аутентификатор — то есть стереть запечатанное.
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewUsers(gate, nil).VerifyCode(ctx, login, code, time.Now()); err == nil {
		t.Fatal("вход прошёл при потерянном ключе запечатывания")
	}
}
