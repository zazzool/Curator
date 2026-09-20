package audience

import (
	"os"
	"regexp"
	"testing"
)

// Словарь признаков живёт в двух местах: здесь и в студии, где у каждого
// признака есть название словами. Два места для одного словаря расходятся
// молча — признак, забытый в студии, нельзя было бы поставить в правило, и
// причину искали бы в ручке, а не в списке.
//
// Сверка односторонней не бывает: она обязана ловить и лишнее в студии
// (признак, которого на сервере нет, поставится в правило и будет
// отвергнут при сохранении), и недостающее, и переставленное — порядок
// задаёт порядок на экране.
//
// Отказ, а не пропуск: недоступный исходник студии значит, что проверка
// ничего не сверила, и объявлять её пройденной нельзя.
//
// Свой прогон зовётся только с -count=1: читаемый файл лежит вне каталога
// пакета, и кэш `go test` правку такого файла не замечает — отвечает
// «ok (cached)» на разошедшемся словаре. Сторож зовёт проверки именно так.
const studioTraitsFile = "../../../web/src/traits.ts"

var (
	studioTraitLine  = regexp.MustCompile(`code:\s*'([a-z-]+)',\s*\n\s*title:`)
	studioWindowLine = regexp.MustCompile(`\{\s*code:\s*'([a-z0-9]+)',\s*title:\s*'за`)
)

func TestСловарьПризнаковСтудииЗеркаленСерверному(t *testing.T) {
	found := studioTraitLine.FindAllStringSubmatch(читайСтудию(t), -1)
	if len(found) == 0 {
		t.Fatalf("в %s не нашлось ни одного признака: "+
			"сверка сломана, а не словарь пуст", studioTraitsFile)
	}

	shown := make([]string, 0, len(found))
	for _, one := range found {
		shown = append(shown, one[1])
	}
	if len(shown) != len(Traits) {
		t.Fatalf("на сервере %d признаков, в студии %d: %v против %v",
			len(Traits), len(shown), Traits, shown)
	}
	for i, trait := range Traits {
		if shown[i] != trait {
			t.Errorf("признак %d: на сервере %q, в студии %q "+
				"(порядок значим — он задаёт порядок на экране)", i, trait, shown[i])
		}
	}
}

func TestСловарьОконСтудииЗеркаленСерверному(t *testing.T) {
	// Окна сверяются отдельно от признаков и по той же причине: окно,
	// которого на сервере нет, даст правило, отвергаемое при сохранении,
	// а недостающее просто нельзя будет выбрать.
	found := studioWindowLine.FindAllStringSubmatch(читайСтудию(t), -1)
	shown := make([]string, 0, len(found))
	for _, one := range found {
		shown = append(shown, one[1])
	}
	if len(shown) != len(Windows) {
		t.Fatalf("на сервере %d окон, в студии %d: %v против %v",
			len(Windows), len(shown), Windows, shown)
	}
	for i, window := range Windows {
		if shown[i] != window {
			t.Errorf("окно %d: на сервере %q, в студии %q", i, window, shown[i])
		}
	}
}

func читайСтудию(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(studioTraitsFile)
	if err != nil {
		t.Fatalf("исходник студии %s не прочитан: %v. "+
			"Это отказ, а не пропуск: несверенный словарь расходится молча",
			studioTraitsFile, err)
	}
	return string(raw)
}
