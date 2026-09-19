package studio

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode"
	"unicode/utf8"
)

// Routes объявляет дверь студии: вход, выход и «кто я».
//
// Дверь открыта всем намеренно, и это единственные три маршрута, о которых
// так сказано вслух: войти нельзя, не будучи снаружи. Всё остальное
// объявляется через Handle, то есть с правом первым доводом.
func Routes(d *Desk) {
	d.Public("POST /admin/api/login", d.handleLogin)
	d.Public("POST /admin/api/logout", d.handleLogout)

	// «Кто я» закрыто правом мастерской? Нет: его спрашивает сама студия
	// сразу после входа, чтобы знать, какие разделы показывать. Право здесь
	// было бы правом «быть собой», а такого права в словаре нет и заводить
	// его незачем — проверка на вход тут и так стоит, её делает сессия.
	d.Public("GET /admin/api/me", d.handleMe)
}

type loginRequest struct {
	Login string `json:"login"`
	Code  string `json:"code"`
}

func (d *Desk) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран")
		return
	}
	token, err := d.Login(r.Context(), req.Login, req.Code)
	if err != nil {
		// Одно и то же сообщение на неизвестное имя и на негодный код:
		// разные ответы рассказали бы постороннему, какое имя существует.
		WriteError(w, http.StatusUnauthorized, "Имя или код не подошли")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (d *Desk) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Выход без сессии — не ошибка: человек, нажавший «выйти» дважды,
	// хотел выйти, и он вышел.
	if token := bearer(r); token != "" {
		if err := d.sessions.Close(r.Context(), token); err != nil {
			WriteError(w, http.StatusInternalServerError, "Выйти не удалось, попробуйте ещё раз")
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Desk) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := d.sessions.User(r.Context(), d.users, bearer(r), d.now())
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "Войдите в студию заново")
		return
	}
	// Права уезжают в ответ списком, и пустой список — [], а не null:
	// студия ходит по нему циклом, и null роняет её на человеке без прав.
	perms := make([]string, 0, len(user.Permissions))
	for _, p := range user.Permissions {
		perms = append(perms, string(p))
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"login":       user.Login,
		"displayName": user.DisplayName,
		"permissions": perms,
	})
}

// DecodeBody разбирает тело запроса, отказывая на лишних полях.
//
// Отказ на незнакомом поле — то же правило, что «непонятое не применяется»:
// студия, пославшая поле с опечаткой, иначе получила бы успешный ответ и
// молча потеряла то, что человек ввёл.
func DecodeBody(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return errors.New("запрос не разобран: " + err.Error())
	}
	return nil
}

// Sentence поднимает первую букву отказа.
//
// Отказы собираются из кусков («единицы %q в источнике нет: …»), и наружу
// уезжает готовая фраза. С маленькой буквы она читается как обрывок
// журнала, а человеку показывают предложение.
//
// Живёт здесь, а не в каждом пакете ручек: правило одно — «текст отказа
// показывается человеку как есть», — и трёх его копий быть не должно.
// Двумя оно уже успело стать, и это тот случай, когда копии расходятся
// молча: первая была написана через вычитание 32 из байта и на кириллице
// давала мусор.
func Sentence(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
