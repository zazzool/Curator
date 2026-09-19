package source

import (
	"fmt"
	"strings"
)

// PathSeparator — разделитель в пути единицы.
//
// Косая черта выбрана потому, что в метках справочников и в нумерации
// пунктов она не встречается. Метка с разделителем внутри — отказ разбора,
// а не молча испорченный путь: путь, в котором разделитель означает то одно,
// то другое, ломает срез, а срезом выбираются задачи.
const PathSeparator = "/"

// BuildPaths считает путь и глубину каждой единицы по связи с родителем.
//
// Это замена разбору метки. Сегодня у МКБ «раздел» и «рубрика» вычислялись
// бы отрезанием знаков от кода; здесь они получаются срезом пути, и для
// приказа с пунктом «3.2.1» работает то же правило, что для F32.1.
//
// # Почему отказ, а не пропуск негодной единицы
//
// Источник с потерянной веткой выглядит исправным: единицы на месте, задачи
// по ним пишутся, а в подборе их нет, потому что путь не тот. Такую беду
// находят через месяц по жалобе «почему по этому приказу ничего не
// выпадает». Поэтому разбор отказывает целиком и называет причину — это
// правило «непонятое не применяется» в его буквальном виде.
func BuildPaths(units []Unit) ([]Unit, error) {
	byLabel := make(map[string]int, len(units))
	for i, u := range units {
		if strings.TrimSpace(u.Label) == "" {
			return nil, fmt.Errorf("единица %d без метки", i+1)
		}
		if strings.Contains(u.Label, PathSeparator) {
			return nil, fmt.Errorf("метка %q содержит %q: это разделитель пути, и в метке его быть не может",
				u.Label, PathSeparator)
		}
		if _, dup := byLabel[u.Label]; dup {
			return nil, fmt.Errorf("метка %q встречается дважды: две единицы под одной меткой — два источника правды об одном понятии", u.Label)
		}
		byLabel[u.Label] = i
	}

	out := make([]Unit, len(units))
	copy(out, units)

	// Путь считается для каждой единицы подъёмом к корню. Длина ветки в
	// справочниках — четыре-пять уровней, так что подъём дешёв; зато
	// рекурсии с общим состоянием здесь нет, и цикл ловится на месте, а не
	// переполнением стека.
	for i := range out {
		chain := []string{}
		seen := map[string]bool{}
		label := out[i].Label
		for label != "" {
			if seen[label] {
				return nil, fmt.Errorf("метка %q стоит в цепочке родителей сама у себя: источник с петлёй разобрать нельзя", out[i].Label)
			}
			seen[label] = true
			chain = append(chain, label)

			idx, ok := byLabel[label]
			if !ok {
				return nil, fmt.Errorf("единица %q ссылается на родителя %q, которого в источнике нет",
					out[i].Label, label)
			}
			label = strings.TrimSpace(out[idx].ParentLabel)
		}

		// Цепочка собрана снизу вверх — разворачиваем: путь читается от
		// корня, и срез по началу строки работает только так.
		for l, r := 0, len(chain)-1; l < r; l, r = l+1, r-1 {
			chain[l], chain[r] = chain[r], chain[l]
		}
		out[i].Path = strings.Join(chain, PathSeparator)
		out[i].Depth = len(chain) - 1
	}
	return out, nil
}

// Slice — срез единиц под указанным путём, вместе с самой единицей этого
// пути.
//
// Проверяется не просто начало строки: путь «F3» иначе захватил бы «F30»,
// которого под ним нет. Совпадением считается либо сам путь, либо путь с
// разделителем после него.
func Slice(units []Unit, path string) []Unit {
	out := []Unit{}
	if path == "" {
		out = append(out, units...)
		return out
	}
	prefix := path + PathSeparator
	for _, u := range units {
		if u.Path == path || strings.HasPrefix(u.Path, prefix) {
			out = append(out, u)
		}
	}
	return out
}
