// Порождение ключа подписи наборов.
//
// Ключ нужен один раз на установку, и порождать его руками нечем:
// openssl отдаёт ed25519 в обёртке PKCS#8, а сервер ждёт голые 64 байта в
// base64. Утилита здесь, а не строчкой в документации, потому что строчку
// переписывают с ошибкой и узнают об этом на первом выпуске набора.
//
// Ключ донора сюда не переносится: приложение Куратора новое, его сборки
// подписаны своим ключом, и общий ключ связал бы два продукта там, где
// связь не нужна никому.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

// generate порождает пару и отдаёт её в том виде, в каком её ждут: обе
// половины строками base64. Отдельной функцией, чтобы проверка могла
// сверить этот вид с тем, что разбирает сервер, — два места для одного
// вида расходятся молча, и заметно это стало бы на первом выпуске.
func generate() (priv, pub string, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(privKey),
		base64.StdEncoding.EncodeToString(pubKey), nil
}

func main() {
	keyID := flag.String("id", "key-1", "имя ключа: уезжает в каждый выпуск")
	flag.Parse()

	priv, pub, err := generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ключ не порождён: %v\n", err)
		os.Exit(1)
	}

	// Закрытая половина печатается готовой строкой .env, открытая —
	// готовым доводом сборки приложения: переписывать их руками из
	// другого вида и есть то место, где теряют один знак.
	fmt.Printf("PACK_SIGNING_KEY=%s\n", priv)
	fmt.Printf("PACK_SIGNING_KEY_ID=%s\n", *keyID)
	fmt.Println()
	fmt.Printf("В сборку приложения:\n  --dart-define=CURATOR_PACK_KEYS=%s:%s\n",
		*keyID, pub)
	fmt.Println()
	fmt.Println("Закрытую половину положите в .env контура и сотрите отсюда.")
	fmt.Println("Второй раз её не покажет никто: ключ порождается заново,")
	fmt.Println("и выпущенные прежним ключом наборы остаются проверяемыми")
	fmt.Println("только пока жив прежний ключ.")
}
