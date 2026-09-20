package studio

import (
	"os"
	"regexp"
	"testing"
)

// Словарь прав живёт в двух местах: здесь и в студии, где у каждого права
// есть название словами. Два места для одного словаря расходятся молча —
// право, забытое в студии, просто нельзя было бы выдать, и причину искали
// бы в маршруте, а не в списке.
//
// Сверка односторонней не бывает: она обязана ловить и лишнее в студии
// (право, которого на сервере нет, выдаётся и отвергается ручкой), и
// недостающее, и переставленное — порядок задаёт порядок на экране.
//
// Отказ, а не пропуск: недоступный исходник студии значит, что проверка
// ничего не сверила, и объявлять её пройденной нельзя.
//
// Свой прогон зовётся только с -count=1: читаемый файл лежит вне каталога
// пакета, и кэш `go test` правку такого файла не замечает — отвечает
// «ok (cached)» на разошедшемся словаре. Довод и случай, на котором это
// поймали, записаны в tools/checks.sh; сторож зовёт проверки именно так.
const studioPermissionsFile = "../../../web/src/permissions.ts"

var studioPermissionLine = regexp.MustCompile(`\{\s*code:\s*'([^']+)'`)

func TestСловарьПравСтудииЗеркаленСерверному(t *testing.T) {
	raw, err := os.ReadFile(studioPermissionsFile)
	if err != nil {
		t.Fatalf("исходник студии %s не прочитан: %v. "+
			"Это отказ, а не пропуск: несверенный словарь расходится молча",
			studioPermissionsFile, err)
	}

	found := studioPermissionLine.FindAllStringSubmatch(string(raw), -1)
	if len(found) == 0 {
		t.Fatalf("в %s не нашлось ни одного права: "+
			"сверка сломана, а не словарь пуст", studioPermissionsFile)
	}

	shown := make([]string, 0, len(found))
	for _, one := range found {
		shown = append(shown, one[1])
	}

	if len(shown) != len(All) {
		t.Fatalf("на сервере %d прав, в студии %d: %v против %v",
			len(All), len(shown), All, shown)
	}
	for i, p := range All {
		if shown[i] != string(p) {
			t.Errorf("право %d: на сервере %q, в студии %q "+
				"(порядок значим — он задаёт порядок на экране)", i, p, shown[i])
		}
	}
}
