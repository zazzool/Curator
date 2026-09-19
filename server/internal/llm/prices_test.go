package llm

import (
	"context"
	"testing"
)

func TestНазваннуюЦенуНеПересчитывают(t *testing.T) {
	// Названная поставщиком цена учитывает и скидку префиксного кэша, и
	// наценку шлюза, а прайс не знает ни того, ни другого.
	prices := Prices{"модель": {Prompt: 1000, Completion: 2000}}
	got, exact := prices.Estimate(Usage{
		PromptTokens: 1000, CompletionTokens: 1000,
		CostUSD: 0.5, CostExact: true,
	}, "модель")
	if !exact || got != Nano/2 {
		t.Fatalf("названная цена пересчитана: %d, точная=%v", got, exact)
	}
}

func TestКэшированныйТокенНеСчитаетсяДважды(t *testing.T) {
	// Кэшированные токены входят в общее число входных, и цена у них своя.
	// Сложить их сверху значит посчитать один и тот же токен дважды.
	prices := Prices{"модель": {Prompt: 100, Completion: 200, CacheRead: 10, CacheWrite: 125}}
	got, exact := prices.Estimate(Usage{
		PromptTokens: 100, CachedTokens: 60, CacheWriteTokens: 20, CompletionTokens: 10,
	}, "модель")
	// Обычных входных остаётся двадцать.
	want := int64(20*100 + 60*10 + 20*125 + 10*200)
	if got != want {
		t.Fatalf("посчитано %d вместо %d", got, want)
	}
	if exact {
		t.Fatal("расчёт по прайсу выдан за названную цену")
	}
}

func TestБезЦеныКэшаСчитаетсяПоВходу(t *testing.T) {
	// Ноль означает «поставщик не сказал». Тогда расчёт завышает — и это
	// правильная сторона для ошибки в оценке: занизив, мы показали бы
	// работу дешевле, чем она есть.
	prices := Prices{"модель": {Prompt: 100, Completion: 100}}
	got, _ := prices.Estimate(Usage{PromptTokens: 10, CachedTokens: 10}, "модель")
	if got != 1000 {
		t.Fatalf("кэш без своей цены посчитан как %d вместо 1000", got)
	}
}

func TestМоделиНетВПрайсе_ЭтоНеБесплатно(t *testing.T) {
	// Ноль здесь означает «не знаем», и помечен он как оценка. Показать
	// бесплатной работу, за которую заплачено, хуже, чем не показать
	// числа вовсе.
	got, exact := Prices{}.Estimate(Usage{PromptTokens: 1000}, "незнакомая")
	if got != 0 || exact {
		t.Fatalf("незнакомая модель посчитана как %d, точная=%v", got, exact)
	}
}

func TestЦенаЧитаетсяСтрокойИзПрайса(t *testing.T) {
	// Цены в ответе — строки, а не числа: доли цента за токен в double
	// превращаются в 0.0000029999999999999997, и вендоры присылают их
	// текстом, чтобы не спорить о последнем знаке.
	client, _, _ := provider(t, KindOpenAI, answer{status: 200, body: `{"data":[
		{"id":"Вендор/Модель","pricing":{"prompt":"0.000003","completion":"0.000015",
		  "input_cache_read":"0.0000003","input_cache_write":"0.00000375"}},
		{"id":"без-цены","pricing":{"prompt":"0","completion":"0"}},
		{"id":"не-фиксирована","pricing":{"prompt":"-1","completion":"-1"}}
	]}`})
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	price, ok := prices["вендор/модель"]
	if !ok {
		t.Fatalf("модель не попала в прайс: %v", prices)
	}
	if price.Prompt != 3000 || price.Completion != 15000 || price.CacheRead != 300 {
		t.Fatalf("цена разобрана не так: %+v", price)
	}
	// Модель без цены входа и выхода в прайсе бесполезна: она дала бы
	// оценку в ноль, неотличимую от бесплатной.
	if _, ok := prices["без-цены"]; ok {
		t.Fatal("модель без цены попала в прайс")
	}
	// «-1» означает «цена не фиксирована»: считать по ней нельзя, и
	// приписывать модели отрицательную цену тем более.
	if _, ok := prices["не-фиксирована"]; ok {
		t.Fatal("модель с нефиксированной ценой попала в прайс")
	}
}

func TestПрайсБезКлючаНеСпрашивается(t *testing.T) {
	client := NewClient(ProviderConfig{Name: "без ключа", BaseURL: "http://127.0.0.1:1"},
		"https://curator.example", "Куратор")
	if _, err := client.FetchPrices(context.Background()); err != ErrNotConfigured {
		t.Fatalf("прайс без ключа спросили: %v", err)
	}
}
