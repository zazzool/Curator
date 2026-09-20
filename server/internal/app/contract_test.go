package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"curator/server/internal/casestore"
)

// Сверка с записанным эталоном ответов.
//
// # Отсутствие эталона роняет проверку, а не пропускает её
//
// Эталон — единственное, что держит формат /v1 неизменным: на руках у
// врачей стоят сборки, которые обновятся не завтра, а компилятор
// рассинхрон не поймает — Go и Dart живут в разных экосистемах. Проверка,
// молча пропустившаяся из-за пропавшего файла, выдала бы зелёное за
// непроверенное ровно в том месте, где цена ошибки — неработающее
// приложение у врача.

// contract — эталон, как он записан.
type contract struct {
	Window    string                      `json:"window"`
	Responses map[string]contractResponse `json:"responses"`
	Metrics   contractShape               `json:"metrics"`
	CaseBody  contractShape               `json:"caseBody"`
	Errors    contractShape               `json:"errors"`
}

type contractResponse struct {
	Status int                          `json:"status"`
	Fields map[string]string            `json:"fields"`
	Each   map[string]map[string]string `json:"each"`
}

type contractShape struct {
	Fields   map[string]string            `json:"fields"`
	Each     map[string]map[string]string `json:"each"`
	Optional map[string][]string          `json:"optional"`
	Kinds    []string                     `json:"kinds"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	path := filepath.Join("..", "..", "..", "shared", "wire-contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("эталон ответов %s не прочитан: %v. "+
			"Это отказ, а не пропуск: без эталона формат /v1 не стережёт ничто", path, err)
	}
	var out contract
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("эталон ответов не разобран: %v", err)
	}
	if len(out.Responses) == 0 {
		t.Fatal("в эталоне ответов не описано ни одного ответа")
	}
	return out
}

func TestЭталонОтветовЧитаетсяИНеПуст(t *testing.T) {
	// Сама проверка на то, что эталон на месте: остальные проверки
	// опираются на него, и пропажу надо заметить здесь, а не в виде
	// пятнадцати одинаковых отказов.
	c := loadContract(t)
	for _, want := range []string{
		"GET /v1/health", "POST /v1/devices", "GET /v1/me",
		"GET /v1/content/version", "GET /v1/cases", "GET /v1/cases/{id}",
		"POST /v1/attempts", "GET /v1/progress", "GET /v1/review",
		"GET /v1/signs",
		"GET /v1/packs", "GET /v1/packs/{slug}", "GET /v1/packs/{slug}/cases",
		"GET /v1/reference", "GET /v1/reference/{slug}/units",
		"GET /v1/reference/{slug}/statements",
		"POST /v1/me/email", "POST /v1/me/email/confirm",
		"POST /v1/recovery", "POST /v1/recovery/confirm",
	} {
		if _, ok := c.Responses[want]; !ok {
			t.Errorf("в эталоне нет ответа %q", want)
		}
	}
	if len(c.CaseBody.Fields) == 0 {
		t.Error("в эталоне не описано содержание задачи")
	}
	if len(c.Metrics.Fields) == 0 {
		t.Error("в эталоне не описаны величины каталога")
	}
	if c.Errors.Fields["error"] != "string" {
		t.Error("в эталоне не описан отказ с полем error")
	}
}

// Словарь видов задачи обязан совпасть с эталоном.
//
// Сверка нужна именно двусторонняя, и именно через общий файл. Вид задачи
// решает на сервере, чем сверять ответ — меткой варианта или его текстом
// (casestore.Body.CorrectOption), — а на устройстве решает, понятна ли
// задача вообще. Пока сверки не было, стороны разошлись: сервер считал
// годной задачу-узнавание с вариантом без метки, устройство выбрасывало
// её целиком, и врач не видел её вовсе. Со стороны устройства с тем же
// списком сверяется app/test/case_kind_test.dart — один список, две
// проверки, и разойтись молча больше нечем.
func TestСловарьВидовСовпадаетСЭталоном(t *testing.T) {
	c := loadContract(t)
	if len(c.CaseBody.Kinds) == 0 {
		t.Fatal("в эталоне не записан словарь видов задачи (caseBody.kinds): " +
			"без него стороны снова разойдутся молча")
	}

	ours := map[string]bool{
		casestore.KindRecognise: true,
		casestore.KindAction:    true,
	}
	written := map[string]bool{}
	for _, kind := range c.CaseBody.Kinds {
		written[kind] = true
		if !ours[kind] {
			t.Errorf("эталон знает вид %q, а сервер такого не знает", kind)
		}
	}
	for kind := range ours {
		if !written[kind] {
			t.Errorf("сервер знает вид %q, а в эталоне его нет — "+
				"устройство о нём не узнает", kind)
		}
	}
}

// matchShape сверяет ответ с описанием: все названные поля на месте и
// нужного вида. Лишние поля допустимы — это и есть добавление.
func matchShape(t *testing.T, where string, got map[string]any, fields map[string]string) {
	t.Helper()
	for name, kind := range fields {
		value, ok := got[name]
		if !ok {
			t.Errorf("%s: нет поля %q — формат разошёлся с эталоном", where, name)
			continue
		}
		if actual := kindOf(value); actual != kind {
			t.Errorf("%s: поле %q приехало как %s, а эталон обещает %s",
				where, name, actual, kind)
		}
	}
}

func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
