package gen

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/studio"
)

// Конвейер, каким его видит составитель: узлы по порядку, у каждого своя
// модель и своя цена.
//
// # Почему перечень узлов приходит С СЕРВЕРА
//
// Узлы объявлены здесь (Nodes), и студия их не повторяет. Повторённый
// список разошёлся бы молча — ровно на том узле, который добавили
// последним: экран показал бы конвейер из пяти узлов там, где их шесть,
// и выглядело бы это не поломкой, а конвейером из пяти узлов. Узел,
// которого не видно, не настраивают.
//
// # Почему числа и настройка приходят ВМЕСТЕ
//
// Решают по ним одно решение: где менять модель. Спроси студия числа
// отдельно от заданий, и сошлись бы они на экране по имени узла — то
// есть по строке, а не по строке базы, — и узел без обращений выпал бы
// из одного списка, оставшись в другом.

// NodeView — один узел конвейера для экрана.
type NodeView struct {
	Node string
	Word string

	// PromptID и PromptName — задание этого узла. Пусто — задания нет, и
	// узел не работает вовсе: сказать об этом надо, а не пропустить
	// строку, иначе пропажа задания выглядит как пропажа узла.
	PromptID   string
	PromptName string

	// Model — модель узла; пусто означает «моделью поставщика».
	Model string

	Calls         int64
	Failed        int64
	MedianNanoUSD int64
	MedianMs      int64
	Estimated     int64
}

// NodeStats — расход по узлам за срок. Пусто — учёта нет вовсе.
type NodeStats func(ctx context.Context, since, until time.Time) (map[string]NodeUsage, error)

// NodeUsage — числа одного узла, какими их отдаёт учёт.
type NodeUsage struct {
	Calls         int64
	Failed        int64
	MedianNanoUSD int64
	MedianMs      int64
	Estimated     int64
}

// pipelineDays — за какой срок показываются числа.
//
// Тридцать суток, а не всё время: модель узла меняют, и числа за всё
// время смешали бы работу прежней модели с работой нынешней — то есть
// отвечали бы на вопрос «сколько стоил узел когда-то», когда спрашивают
// «сколько он стоит».
const pipelineDays = 30

func (r *routes) pipeline(w http.ResponseWriter, req *http.Request, _ studio.User) {
	ctx := req.Context()
	prompts, err := r.prompts.All(ctx)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Задания узлов не прочитаны")
		return
	}
	// Источник в запросе — чей конвейер показывать. Пусто или ноль —
	// общий: он и работает там, где источник своего задания не завёл.
	sourceID, _ := strconv.ParseInt(req.URL.Query().Get("source"), 10, 64)

	// Отбор тот же, что у ForNode, и повторён он здесь намеренно одним
	// правилом: своё задание источника старше общего умолчания. Разойдись
	// экран с исполнителем — составитель правил бы задание, мимо которого
	// задачи не ходят, и узнал бы об этом по качеству задач через неделю.
	//
	// Первое по порядку сюда просится само и молчит: список идёт по узлу
	// и по идентификатору, и запасное задание с именем поменьше встаёт
	// ВЫШЕ рабочего. Проверка на это есть.
	byNode := map[string]Prompt{}
	for _, p := range prompts {
		switch {
		case sourceID > 0 && p.SourceID == sourceID:
			byNode[p.Node] = p
		case p.IsDefault:
			if got, taken := byNode[p.Node]; taken && got.SourceID == sourceID {
				// Своё задание уже взято — умолчание его не вытесняет.
				continue
			}
			byNode[p.Node] = p
		}
	}

	until := time.Now()
	since := until.AddDate(0, 0, -pipelineDays)
	usage := map[string]NodeUsage{}
	if r.stats != nil {
		usage, err = r.stats(ctx, since, until)
		if err != nil {
			// Числа — пометка к конвейеру, а не сам конвейер: уронить
			// из-за них экран настройки значит отнять и настройку. Но и
			// молчать нельзя — нули читаются как «узел не работал».
			studio.WriteError(w, http.StatusInternalServerError, "Расход по узлам не прочитан")
			return
		}
	}

	out := make([]map[string]any, 0, len(Nodes))
	for _, node := range Nodes {
		p := byNode[node]
		u := usage[node]
		out = append(out, map[string]any{
			"node": node, "word": NodeWord(node),
			"promptId": p.ID, "promptName": p.Name, "model": p.Model,
			// «Своё» говорится прямо, а не выводится из совпадения
			// идентификаторов в студии: два места для одного правила
			// расходятся молча, и разойдясь, показали бы общее задание
			// как своё — то есть позвали бы править его всем источникам
			// сразу.
			"own":   p.SourceID == sourceID && sourceID > 0,
			"calls": u.Calls, "failed": u.Failed,
			"medianNanoUsd": u.MedianNanoUSD, "medianMs": u.MedianMs,
			"estimated": u.Estimated,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"nodes":  out,
		"source": sourceID,
		// Срок уезжает числом, а не словами в студии: два места для
		// одного числа расходятся молча, и разойдясь, подписали бы
		// «за 30 суток» числа за неделю.
		"days": pipelineDays,
	})
}

// forkPrompt заводит источнику своё задание узла.
func (r *routes) forkPrompt(w http.ResponseWriter, req *http.Request, _ studio.User) {
	sourceID, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Источник назван не числом")
		return
	}
	node := req.PathValue("node")
	if !knownNode(node) {
		// Словарь узлов закрыт, и незнакомое имя — это опечатка в адресе,
		// а не новый узел. Заведи мы задание по нему, оно осталось бы в
		// базе навсегда, не работая никогда.
		studio.WriteError(w, http.StatusBadRequest, "Такого узла в конвейере нет")
		return
	}
	saved, err := r.prompts.Fork(req.Context(), sourceID, node)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"promptId": saved.ID, "node": saved.Node, "sourceId": sourceID,
	})
}

// unforkPrompt возвращает узел источника к общему заданию.
func (r *routes) unforkPrompt(w http.ResponseWriter, req *http.Request, _ studio.User) {
	sourceID, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Источник назван не числом")
		return
	}
	if err := r.prompts.Unfork(req.Context(), sourceID, req.PathValue("node")); err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"status": "общее"})
}

// knownNode — есть ли такой узел в конвейере.
//
// По объявленному перечню, а не по тому, что нашлось в базе: задание
// неизвестного узла в базе как раз и завелось бы опечаткой в адресе.
func knownNode(node string) bool {
	for _, one := range Nodes {
		if one == node {
			return true
		}
	}
	return false
}
