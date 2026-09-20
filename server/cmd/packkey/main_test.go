package main

import (
	"crypto/ed25519"
	"testing"

	"curator/server/internal/packs"
)

// Утилита и сервер описывают один и тот же вид ключа в двух местах, и
// расходятся такие пары молча: увидели бы расхождение на первом выпуске
// набора, когда служба уже стоит на контуре.
func TestПорождённыйКлючРазбираетсяСервером(t *testing.T) {
	priv, pub, err := generate()
	if err != nil {
		t.Fatal(err)
	}

	parsedPriv, err := packs.ParsePrivateKey(priv)
	if err != nil {
		t.Fatalf("закрытая половина не разобрана сервером: %v", err)
	}
	parsedPub, err := packs.ParsePublicKey(pub)
	if err != nil {
		t.Fatalf("открытая половина не разобрана сервером: %v", err)
	}

	// Половины сверяются подписью, а не сравнением: именно подписью они
	// и работают. Напечатай мы открытый ключ от другой пары — вышла бы
	// сборка приложения, отвергающая все наши наборы, и узналось бы это
	// только на устройстве врача.
	const manifest = "опись выпуска"
	if !ed25519.Verify(parsedPub, []byte(manifest),
		ed25519.Sign(parsedPriv, []byte(manifest))) {
		t.Fatal("напечатанные половины — не пара: подпись не сходится")
	}
}
