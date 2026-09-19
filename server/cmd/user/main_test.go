package main

import (
	"strings"
	"testing"

	"curator/server/internal/studio"
)

func TestПраваРазбираютсяИлиОтказывают(t *testing.T) {
	all, err := parsePerms("all")
	if err != nil {
		t.Fatalf("«all» не разобралось: %v", err)
	}
	if len(all) != len(studio.All) {
		t.Errorf("«all» дало %d прав из %d", len(all), len(studio.All))
	}

	some, err := parsePerms(" source:read , case:write ")
	if err != nil {
		t.Fatalf("список прав не разобрался: %v", err)
	}
	if len(some) != 2 {
		t.Errorf("прав разобралось %d, а названо было 2: %v", len(some), some)
	}

	// Опечатка в праве — отказ, а не пропуск: право, молча выброшенное
	// при заведении, оставит человека без раздела, и искать причину будут
	// в маршруте, а не в доводе командной строки.
	if _, err := parsePerms("source:read,case:wrait"); err == nil {
		t.Error("право с опечаткой разобралось молча")
	}

	// Пустой список — чаще всего забытый довод, и пользователь из него
	// выходит такой, что войдёт и не увидит ничего.
	err = mustFail(t, "")
	if !strings.Contains(err.Error(), "-perms") {
		t.Errorf("отказ не называет довод, которого не хватает: %v", err)
	}
}

func mustFail(t *testing.T, raw string) error {
	t.Helper()
	perms, err := parsePerms(raw)
	if err == nil {
		t.Fatalf("%q разобралось в %v, а должно было отказать", raw, perms)
	}
	return err
}
