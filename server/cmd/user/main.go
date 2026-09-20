// Пользователи студии.
//
// # Зачем отдельная утилита
//
// Пользователей заводит мастерская — и заводит их пользователь с правом
// workshop. На свежем контуре такого нет ни одного, и войти, чтобы завести
// первого, нельзя по кругу. Эта утилита разрывает круг, и только его:
// дальше пользователи заводятся в студии, как и задумано.
//
// Посев переменными окружения (логин и секрет в .env, читаются при старте,
// «потом можно убрать») здесь не годится. Секрет второго довода, лежащий в
// файле рядом со строкой подключения, — это второй пароль рядом с первым, а
// «потом» наступает ровно никогда.
//
// # Почему в образе, а не в дереве исходников
//
// Там же и потому же, что накат схемы: база видна только из сети сервера.
// Утилита, которую в аварии надо сперва собрать, в аварии бесполезна.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/studio"
	"curator/server/internal/totp"
)

func main() {
	login := flag.String("login", "", "имя входа")
	name := flag.String("name", "", "как звать человека в студии")
	perms := flag.String("perms", "", "права через запятую; all — все")
	list := flag.Bool("list", false, "показать заведённых и их права")
	flag.Parse()

	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("настройки из .env не прочитаны: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL не задан: заводить пользователя негде")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gate, err := dbgate.Open(ctx, dsn, dbgate.Options{})
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	defer gate.Close()

	users := studio.NewUsers(gate)

	if *list {
		showAll(ctx, gate)
		return
	}

	if strings.TrimSpace(*login) == "" {
		fmt.Fprintln(os.Stderr, "чего не хватает: -login. Права перечисляются через -perms,")
		fmt.Fprintln(os.Stderr, "список заведённых — -list. Например:")
		fmt.Fprintln(os.Stderr, "  /app/user -login editor -name «Составитель» -perms all")
		os.Exit(2)
	}

	chosen, err := parsePerms(*perms)
	if err != nil {
		log.Fatal(err)
	}

	display := strings.TrimSpace(*name)
	if display == "" {
		display = *login
	}

	user, secret, err := users.Create(ctx, *login, display, chosen)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Секрет показывается ровно здесь и больше нигде: хранится он, чтобы
	// сверять коды, а показанный второй раз превращается во второй пароль.
	// Поэтому рядом печатается и ссылка привязки — набирать секрет руками
	// человек не станет, а ошибётся молча.
	fmt.Printf("заведён %s (%s), прав: %d\n", user.Login, user.DisplayName, len(user.Permissions))
	fmt.Printf("секрет: %s\n", secret)
	fmt.Printf("ссылка привязки: %s\n", totp.URI("Куратор", user.Login, secret))
	fmt.Println()
	fmt.Println("Секрет показан один раз. Привяжите аутентификатор сейчас:")
	fmt.Println("потерянный секрет не восстанавливается, пользователь заводится заново.")
}

// parsePerms разбирает права, отказывая на незнакомом.
//
// Пустой список — это пользователь, который войдёт и не увидит ничего.
// Отказываем: чаще всего это забытый довод, а не замысел, и выясняется он
// через полчаса после того, как человеку отдали ссылку привязки.
func parsePerms(raw string) ([]studio.Permission, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("не названо ни одного права (-perms). "+
			"Все разом — -perms all; по одному — через запятую, например %q",
			string(studio.PermSourceRead)+","+string(studio.PermCaseWrite))
	}
	if raw == "all" {
		return studio.All, nil
	}
	parts := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return studio.ParsePermissions(cleaned)
}

// showAll перечисляет заведённых.
//
// Запрос здесь свой, а не в studio: студии список пользователей отдаёт
// мастерская со своими правами, а это утилита контура, и ей нужно ровно
// «кто заведён» — без сессий, без секретов.
func showAll(ctx context.Context, gate *dbgate.Gate) {
	rows, err := gate.Query(ctx,
		`SELECT login, display_name, permissions FROM users ORDER BY id`)
	if err != nil {
		log.Fatalf("список пользователей не прочитан: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var login, display string
		var perms []string
		if err := rows.Scan(&login, &display, &perms); err != nil {
			log.Fatalf("строка пользователя не разобрана: %v", err)
		}
		count++
		fmt.Printf("%-16s %-24s %s\n", login, display, strings.Join(perms, " "))
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("список пользователей не дочитан: %v", err)
	}
	if count == 0 {
		fmt.Println("не заведено ни одного пользователя: в студию войти нельзя.")
		fmt.Println("Первый заводится этой же утилитой — см. -login.")
	}
}
