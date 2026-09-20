#!/usr/bin/env bash
# Заводит автоматическую выкатку: ключ от контура и два секрета в
# репозитории на Forgejo. Гоняется ОДИН раз и с машины, у которой уже есть
# доступ к контуру по ssh, — то есть с машины владельца.
#
# # Почему Forgejo, а не GitHub
#
# Выкатывает Forgejo (решение владельца 20.09.2026): он стоит внутри
# периметра владельца, и ключ от боевого контура не уезжает к GitHub
# вовсе. GitHub остаётся сторожем проверок. Разбор — docs/deploy.md.
#
# # Почему это не делает сессия
#
# Ключ, которым сторож ходит на контур, порождается здесь и уезжает прямо
# в секреты репозитория, минуя чей-либо экран, разговор и переписку.
# Сессия сделать этого не может и не должна: у неё нет доступа ни к
# контуру, ни к Forgejo (оба закрыты политикой выхода её окружения), а
# закрытая половина ключа, показанная в чате, уже не закрытая.
#
# # Что остаётся после
#
# На контуре — одна строка в authorized_keys для root. В репозитории на
# Forgejo — два секрета: CURATOR_SSH_KEY (закрытая половина) и
# CURATOR_SSH_KNOWN_HOSTS (отпечаток хоста). Закрытая половина на этой
# машине НЕ остаётся: каталог с ней стирается при выходе, как бы скрипт ни
# кончился.
set -euo pipefail

HOST="${CURATOR_HOST:-root@curator.psync.ru}"
FORGEJO_URL="${FORGEJO_URL:-https://exam.psync.ru}"
FORGEJO_REPO="${FORGEJO_REPO:-zazzool/Curator}"

die() {
    printf '%s\n' "$@" >&2
    exit 1
}

command -v ssh-keygen >/dev/null || die "Нет ssh-keygen."
command -v ssh-keyscan >/dev/null || die "Нет ssh-keyscan."
command -v curl >/dev/null || die "Нет curl."

# Токен спрашивается, а не берётся из аргументов: строка запуска попадает
# в историю оболочки и в список процессов, и токен Forgejo пережил бы там
# эту сессию.
if [ -z "${FORGEJO_TOKEN:-}" ]; then
    printf 'Токен Forgejo (Settings → Applications → Access Tokens, права write:repository): '
    read -r -s FORGEJO_TOKEN
    printf '\n'
fi
[ -n "$FORGEJO_TOKEN" ] || die "Без токена секреты завести нечем."

API="$FORGEJO_URL/api/v1/repos/$FORGEJO_REPO"

# Токен проверяется ДО того, как что-либо меняется: узнать, что он не
# годится, после записи ключа на контур — значит чинить половину
# сделанного.
echo "== Проверяю доступ к $FORGEJO_REPO на Forgejo =="
curl -fsS -o /dev/null -H "Authorization: token $FORGEJO_TOKEN" "$API" ||
    die "Forgejo не отвечает или токен не годится: $API" \
        "Проверьте FORGEJO_URL, FORGEJO_REPO и права токена (write:repository)."

# Имя хоста отдельно от имени пользователя: ssh-keyscan спрашивает хост, а
# в HOST он приходит вместе с root@.
HOSTNAME_ONLY="${HOST#*@}"
# Порт, если он задан как root@host:2222 — ssh-keyscan просит его отдельно.
PORT=22
case "$HOSTNAME_ONLY" in
*:*)
    PORT="${HOSTNAME_ONLY##*:}"
    HOSTNAME_ONLY="${HOSTNAME_ONLY%%:*}"
    ;;
esac

echo "Контур:      $HOST"
echo "Forgejo:     $FORGEJO_URL/$FORGEJO_REPO"
echo

# Каталог стирается при любом выходе, включая Ctrl-C: закрытая половина
# ключа не должна пережить этот скрипт ни на минуту.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

echo "== Порождаю ключ =="
# Без пароля намеренно: пароль у ключа, которым пользуется сторож, значил
# бы, что пароль лежит вторым секретом рядом. Защита ключа здесь — в том,
# что закрытую половину видит только Forgejo.
ssh-keygen -t ed25519 -N '' -C "curator-deploy (Forgejo)" -f "$WORK/key" >/dev/null
echo "Открытая половина: $(cat "$WORK/key.pub")"
echo

echo "== Кладу открытую половину на контур =="
# Не ssh-copy-id: он зовёт ssh-add и ведёт себя по-разному на macOS и
# Linux. Здесь достаточно дописать строку, и важно именно дописать —
# authorized_keys на контуре уже не пуст, и перезапись отрезала бы
# владельца от собственного сервера.
ssh "$HOST" 'mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys' \
    < "$WORK/key.pub" ||
    die "Не удалось записать ключ на контур. Проверьте, что ssh $HOST работает."

echo "== Снимаю отпечаток хоста =="
ssh-keyscan -p "$PORT" -t rsa,ecdsa,ed25519 "$HOSTNAME_ONLY" > "$WORK/known_hosts" 2>/dev/null ||
    die "ssh-keyscan не ответил для $HOSTNAME_ONLY:$PORT."
# Пустой файл отпечатков — самый опасный исход: секрет завёлся бы, а
# первая же выкатка отказала бы на «host key verification failed», и
# лечить это стали бы отключением проверки хоста.
[ -s "$WORK/known_hosts" ] || die "Отпечаток хоста пуст — $HOSTNAME_ONLY:$PORT не отвечает по ssh."

echo "== Проверяю ключ делом =="
# До секретов, а не после: секрет, который не работает, хуже отсутствующего
# — он выглядит настроенным, и разбираться с ним будут на горящем контуре.
ssh -i "$WORK/key" -o IdentitiesOnly=yes -o BatchMode=yes \
    -o UserKnownHostsFile="$WORK/known_hosts" -o StrictHostKeyChecking=yes \
    "$HOST" 'docker compose version >/dev/null && echo "контур отвечает, docker на месте"' ||
    die "Новый ключ на контур не пускает (или на контуре нет docker compose)." \
        "Секреты не заведены — чинить нечего, можно просто повторить."

# Значение секрета уезжает телом запроса в JSON, и его надо закодировать.
# Ни python, ни jq для этого не зовутся: их нет на части машин, а здесь
# хватает замены переводов строк. Это безопасно ИМЕННО для этих двух
# значений и ни для каких других: ключ OpenSSH и вывод ssh-keyscan состоят
# из base64, пробелов и дефисов — ни кавычек, ни обратных косых в них нет
# и быть не может.
json_lines() {
    awk '{printf "%s\\n", $0}' "$1"
}

put_secret() {
    local name="$1" file="$2"
    printf '{"data":"%s"}' "$(json_lines "$file")" |
        curl -fsS -o /dev/null -X PUT \
            -H "Authorization: token $FORGEJO_TOKEN" \
            -H 'Content-Type: application/json' \
            --data-binary @- \
            "$API/actions/secrets/$name" ||
        die "Не удалось записать секрет $name в $FORGEJO_REPO." \
            "Нужны права write:repository у токена."
    echo "  $name — записан"
}

echo "== Кладу секреты в $FORGEJO_REPO на Forgejo =="
put_secret CURATOR_SSH_KEY "$WORK/key"
put_secret CURATOR_SSH_KNOWN_HOSTS "$WORK/known_hosts"

cat <<DONE

Готово. Дальше выкатывает Forgejo: всё, что ложится в его главную ветку,
уезжает на $HOST само — если проверки на GitHub по тому же коммиту зелены.

Толкнуть руками — «Выкатка» → запустить вручную, в действиях репозитория
на Forgejo. Там же и возврат предыдущего образа, когда контур уже горит.

Чего не делает этот скрипт: не заводит сам репозиторий на Forgejo и не
доставляет туда слияния, сделанные на GitHub. Это отдельный шаг, и он
описан в docs/deploy.md — без него выкатке нечего выкатывать.

Отозвать доступ, если ключ понадобится убрать:
    ssh $HOST "sed -i '/curator-deploy (Forgejo)/d' ~/.ssh/authorized_keys"
DONE
