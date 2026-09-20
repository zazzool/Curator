package rules

import (
	"fmt"
	"strings"
	"time"
)

// Book — свод в работе: СНИМОК на один заход, а не ссылка на хранилище.
//
// Снимок, а не живое чтение, и это не бережливость. Написание задачи —
// это минуты, за которые правило может дозреть до кворума на соседнем
// задании. Задача, написанная по одному своду и проверенная по другому,
// получила бы замечание о правиле, которого в её задании не было, — и
// объяснить это составителю нечем.
type Book struct {
	rules []Rule
	now   time.Time
}

// NewBook собирает свод из прочитанных правил.
func NewBook(list []Rule, now time.Time) Book {
	cp := make([]Rule, len(list))
	copy(cp, list)
	return Book{rules: cp, now: now}
}

// Len — сколько правил в снимке, всех состояний.
func (b Book) Len() int { return len(b.rules) }

// Applicable — правила, действующие при этих обстоятельствах.
func (b Book) Applicable(ctx Context) []Rule {
	out := make([]Rule, 0, len(b.rules))
	for _, r := range b.rules {
		if !r.InForce() || r.faded(b.now) || !r.Scope.matches(ctx) {
			continue
		}
		// Род правила — свойство самого правила, а не его области,
		// поэтому отбор по нему стоит здесь, а не в matches.
		if len(ctx.Kinds) > 0 && !hasKind(ctx.Kinds, r.Kind) {
			continue
		}
		out = append(out, r)
	}
	sortRules(out)
	return out
}

// Block собирает блок правил для задания.
//
// Два ведра, и это главное отличие от плоского списка. Общие правила
// накапливаются быстрее частных — их подтверждает любая задача, — и в
// одном списке они вытеснили бы правила про конкретный источник
// подчистую. Между тем ценность обратная: общее модель знает и из
// самого задания, а «в этом приказе очная форма называется явкой» ей
// больше взять неоткуда.
//
// Частные идут первыми: начало списка модель удерживает лучше конца, и
// если чем-то придётся пренебречь, пусть это будет общее место.
//
// Пустой свод даёт пустую строку, а не заголовок без содержимого:
// «ПРАВИЛА:» с пустотой под ним модель истолкует по-своему — вернее
// всего, примет за требование следующий абзац.
func (b Book) Block(ctx Context) string {
	scoped, core := b.pick(ctx)
	if len(scoped) == 0 && len(core) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("ПРАВИЛА НАБОРА. Это не пожелания: каждый пункт — либо " +
		"требование составителя, либо ошибка, которую уже приходилось " +
		"править руками.\n")

	if len(scoped) > 0 {
		sb.WriteString("\nПро эту задачу:\n")
		for _, r := range scoped {
			fmt.Fprintf(&sb, "- %s\n", r.Text)
		}
	}
	if len(core) > 0 {
		if len(scoped) > 0 {
			sb.WriteString("\nВсегда:\n")
		} else {
			sb.WriteString("\n")
		}
		for _, r := range core {
			fmt.Fprintf(&sb, "- %s\n", r.Text)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// pick разбирает подходящие правила на частные и общие, укладывая каждую
// половину в свой бюджет.
//
// Бюджет считается в БАЙТАХ — той же мерой, что и потолок текста правила
// (TextMax). Кириллическая буква стоит двух, и мера эта пессимистична
// вдвое; так и задумано: у потолка блока цена ошибки односторонняя —
// перебрав, платишь в каждом запросе.
func (b Book) pick(ctx Context) (scoped, core []Rule) {
	// Отбор идёт ОДНИМ проходом в порядке старшинства, а не двумя
	// независимыми. Наполняй каждое ведро само по себе — и общий потолок
	// числа правил урезался бы с конца, то есть за счёт общих:
	// шестнадцать частных правил, выведенных моделью, вынесли бы из
	// задания ВСЕ общие, включая написанные составителем и встроенные.
	// Старшинство источника тут не спасает — оно действует внутри ведра,
	// а вёдра между собой не соревнуются.
	spent := map[bool]int{} // true — частное ведро
	limits := map[bool]int{true: scopedChars, false: coreChars}

	for _, r := range b.Applicable(ctx) {
		if r.Text == "" {
			continue
		}
		if len(scoped)+len(core) >= blockLimit {
			break
		}
		narrow := !r.Scope.always()
		cost := len(r.Text) + 3 // «- » и перевод строки
		if spent[narrow]+cost > limits[narrow] {
			// Не break: пропустив одно длинное правило, берём следующее
			// подходящее. Байты — мера места, а не очереди, и держать
			// ведро пустым из-за одного многословного правила незачем.
			continue
		}
		spent[narrow] += cost
		if narrow {
			scoped = append(scoped, r)
		} else {
			core = append(core, r)
		}
	}
	return scoped, core
}

func hasKind(list []Kind, want Kind) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}
