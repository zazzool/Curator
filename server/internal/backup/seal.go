// Шифрование снимка перед вывозом.
//
// # Зачем своё, а не «хранилище само шифрует»
//
// Шифрование на стороне хранилища защищает от того, кто унесёт их диск, и
// ни от чего больше: ключ там же, где данные, и всякий, кто получил доступ
// к ведру, получает и содержимое. Снимок нашей базы — это все задачи, вся
// разметка и вся переписка врачей с приложением; он обязан уезжать
// нечитаемым для того, кто его хранит.
//
// # Почему по кускам, а не одним куском
//
// AES-GCM в стандартной библиотеке принимает и отдаёт целое сообщение: на
// снимке в гигабайт это гигабайт в памяти, а контейнеру отведено 512
// мегабайт. Поэтому поток режется на куски, и каждый кусок запечатывается
// отдельно.
//
// Куски при этом нельзя ни переставить, ни выбросить, ни взять из другого
// файла. Держит это в первую очередь одноразовое число: оно и есть номер
// куска, а кусок, запечатанный с одним номером, не распечатывается с
// другим. Тот же номер уходит и в дополнительные данные GCM — не потому,
// что без него не сходится (проверено: сходится), а потому, что тогда всё
// держится на одном соглашении о выборе одноразового числа, и смена этого
// соглашения снимет защиту молча.
//
// Признак последнего куска — отдельное дело, и он не лишний: без него
// файл, у которого отрезали хвост ровно по границе куска, распечатался бы
// как целый.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Размер куска открытого текста.
//
// Мегабайт: на нём память предсказуема, а накладные расходы (по 12 байт
// счётчика и 16 байт печати на кусок) — три стотысячных.
const chunkSize = 1 << 20

// Метка в начале файла: чем запечатано и какой это вид.
//
// Читается тем, кто нашёл файл и не знает, что это. Без метки
// зашифрованный снимок неотличим от испорченного.
var sealMagic = []byte("CURATORSEAL1")

// Seal запечатывает src в dst ключом key.
//
// Ключ ровно в 32 байта: AES-256. Короткий ключ не растягивается и не
// дополняется — «дополним нулями» превращает опечатку в переменной
// окружения в шифрование ключом из одних нулей, и происходит это молча.
func Seal(dst io.Writer, src io.Reader, key []byte) error {
	gcm, err := sealer(key)
	if err != nil {
		return err
	}
	if _, err := dst.Write(sealMagic); err != nil {
		return fmt.Errorf("метка не записана: %w", err)
	}

	plain := make([]byte, chunkSize)
	for counter := uint64(0); ; counter++ {
		n, readErr := io.ReadFull(src, plain)
		// Последний кусок может оказаться и пустым — когда снимок кончился
		// ровно по границе. Пишется он всё равно: он и есть признак конца,
		// и без него файл, у которого отрезали хвост по границе куска,
		// распечатался бы как целый.
		last := readErr == io.EOF || readErr == io.ErrUnexpectedEOF
		if readErr != nil && !last {
			return fmt.Errorf("снимок не дочитан: %w", readErr)
		}

		sealed := gcm.Seal(nil, nonce(counter), plain[:n], header(counter, last))
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(sealed)))
		if _, err := dst.Write(size[:]); err != nil {
			return fmt.Errorf("длина куска не записана: %w", err)
		}
		if _, err := dst.Write(sealed); err != nil {
			return fmt.Errorf("кусок не записан: %w", err)
		}
		if last {
			return nil
		}
	}
}

// Open распечатывает то, что запечатал Seal.
//
// Существует ради проверки, а не ради восстановления: восстанавливает
// человек, и ему нужен тот же код, которым запечатывали, — иначе
// «распечатывается» проверяется только рассуждением. Снимок, который
// никто не распечатывал, — не снимок.
func Open(dst io.Writer, src io.Reader, key []byte) error {
	gcm, err := sealer(key)
	if err != nil {
		return err
	}
	magic := make([]byte, len(sealMagic))
	if _, err := io.ReadFull(src, magic); err != nil {
		return fmt.Errorf("метка не прочитана: %w", err)
	}
	if string(magic) != string(sealMagic) {
		return errors.New("это не запечатанный снимок: метка не та")
	}

	var counter uint64
	for {
		var size [4]byte
		if _, err := io.ReadFull(src, size[:]); err != nil {
			// Конец файла здесь — это обрыв: последний кусок называет
			// себя последним сам, и дочитав его, мы уже вернулись.
			return fmt.Errorf("файл оборван на куске %d: %w", counter, err)
		}
		length := binary.BigEndian.Uint32(size[:])
		if int(length) > chunkSize+gcm.Overhead() {
			return fmt.Errorf("кусок %d называет невозможную длину %d", counter, length)
		}
		sealed := make([]byte, length)
		if _, err := io.ReadFull(src, sealed); err != nil {
			return fmt.Errorf("кусок %d не дочитан: %w", counter, err)
		}

		// Какой это кусок — последний или нет — не записано в файле, и
		// это не забывчивость: запись, которой верят до сверки печати,
		// подделывается. Поэтому пробуются оба толкования, и подходит то,
		// с которым сходится печать.
		plain, err := gcm.Open(nil, nonce(counter), sealed, header(counter, false))
		if err != nil {
			last, lastErr := gcm.Open(nil, nonce(counter), sealed, header(counter, true))
			if lastErr != nil {
				return fmt.Errorf("кусок %d не сошёлся с печатью", counter)
			}
			if _, err := dst.Write(last); err != nil {
				return fmt.Errorf("кусок %d не записан: %w", counter, err)
			}
			return nil
		}
		if _, err := dst.Write(plain); err != nil {
			return fmt.Errorf("кусок %d не записан: %w", counter, err)
		}
		counter++
	}
}

func sealer(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("ключ шифрования длиной %d байт вместо 32", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("шифр не заведён: %w", err)
	}
	return cipher.NewGCM(block)
}

// nonce — счётчик куска, он же одноразовое число.
//
// Счётчиком, а не случайным: случайные числа на многих кусках однажды
// совпадут, а повтор одноразового числа в GCM раскрывает открытый текст.
// Счётчик не повторяется по устройству, и ключ у каждого файла свой не
// нужен ровно до тех пор, пока не повторяется он.
func nonce(counter uint64) []byte {
	out := make([]byte, 12)
	binary.BigEndian.PutUint64(out[4:], counter)
	return out
}

// header — то, что запечатано вместе с куском, но не зашифровано.
//
// Номер куска здесь повторяет одноразовое число намеренно: см. пояснение
// к пакету. Признак последнего куска живёт только здесь.
func header(counter uint64, last bool) []byte {
	out := make([]byte, 9)
	binary.BigEndian.PutUint64(out, counter)
	if last {
		out[8] = 1
	}
	return out
}
