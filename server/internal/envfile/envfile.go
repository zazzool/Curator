// Чтение файла настроек server/.env.
//
// Файл нужен только разработке: на контуре переменные задаёт docker, и
// читать их из файла незачем. Поэтому отсутствие файла — не отказ.
//
// Уже заданное окружение сильнее файла, и это не мелочь: на контуре файл
// может приехать в образ по недосмотру, и переменная из него молча
// перебила бы настройку контура — например, увела бы сервис на чужой адрес.
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// Load читает файл и выставляет из него только те переменные, которых в
// окружении ещё нет.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, already := os.LookupEnv(key); already {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return sc.Err()
}
