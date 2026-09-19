package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPgПовторныйНомерКлючаГоворитСловами(t *testing.T) {
	// Отказ базы приезжал человеку строкой «duplicate key value violates
	// unique constraint "app_keys_pkey" (SQLSTATE 23505)», и по ней не
	// понять ни что случилось, ни что делать. Повтор номера — обычная
	// человеческая ошибка, и говорить о ней надо словами.
	ctx := context.Background()
	keys := NewKeys(testGate(t))

	// Номер свой у каждого прогона: база проверок живёт дольше одного
	// прогона, и постоянный номер отказал бы на втором — причём отказом,
	// который эта же проверка и завела.
	id := fmt.Sprintf("сборка-%d", time.Now().UnixNano())

	first, err := keys.Issue(ctx, id, "Первая")
	if err != nil {
		t.Fatalf("ключ не заведён: %v", err)
	}

	_, err = keys.Issue(ctx, id, "Вторая")
	if err == nil {
		t.Fatal("второй ключ с тем же номером заведён")
	}
	if strings.Contains(err.Error(), "SQLSTATE") {
		t.Errorf("отказ показывает человеку внутренности базы: %v", err)
	}
	if !strings.Contains(err.Error(), "уже заведён") {
		t.Errorf("отказ не говорит, что случилось: %v", err)
	}

	// И главное: прежний ключ остался годным. Перезапись подменила бы
	// отпечаток живого ключа, и все сборки с ним отказали бы разом — молча
	// и не у нас, а на руках у врачей.
	if _, err := keys.Check(ctx, first); err != nil {
		t.Errorf("прежний ключ перестал подходить: %v", err)
	}
}
