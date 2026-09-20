package gen

import (
	"context"
	"net/http"

	"curator/server/internal/studio"
)

// Список моделей для выбора в студии.
//
// Живёт рядом с заданиями намеренно: выбирают модель там же, где правят
// задание узла, и ради одного поля заводить второй раздел настроек
// незачем. Учёта расхода пакет при этом не знает — ему передают готовую
// выборку, и дверью к базе он не обзаводится.

// ModelChoice — одна модель в списке выбора.
type ModelChoice struct {
	Provider string
	Model    string

	// PromptNanoUSD и CompletionNanoUSD — цена одного токена. Ноль
	// означает «цена неизвестна», и в студии это сказано словами: цифра
	// «0 ₽» на месте неизвестной цены выглядит как бесплатно.
	PromptNanoUSD     int64
	CompletionNanoUSD int64
}

// ModelLister отдаёт модели, о которых что-то известно.
//
// Функцией, а не хранилищем: список собирается из прайса, живущего в
// учёте расхода, а знать про учёт конвейеру незачем. Пустой lister —
// законное состояние свежей установки, и отвечает ручка пустым списком,
// а не отказом: выбора нет, но вписать имя модели руками по-прежнему
// можно, и отказ ручки прятал бы поле, которое работает.
type ModelLister func(ctx context.Context) ([]ModelChoice, error)

func (r *routes) listModels(w http.ResponseWriter, req *http.Request, _ studio.User) {
	out := make([]map[string]any, 0)
	if r.models != nil {
		list, err := r.models(req.Context())
		if err != nil {
			studio.WriteError(w, http.StatusInternalServerError, "Список моделей не прочитан")
			return
		}
		for _, one := range list {
			out = append(out, map[string]any{
				"provider": one.Provider, "model": one.Model,
				"promptNanoUsd": one.PromptNanoUSD, "completionNanoUsd": one.CompletionNanoUSD,
			})
		}
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"models": out})
}
