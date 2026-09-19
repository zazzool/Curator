package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
	} {
		if _, ok := c.Responses[want]; !ok {
			t.Errorf("в эталоне нет ответа %q", want)
		}
	}
	if len(c.CaseBody.Fields) == 0 {
		t.Error("в эталоне не описано содержание задачи")
	}
	if c.Errors.Fields["error"] != "string" {
		t.Error("в эталоне не описан отказ с полем error")
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
