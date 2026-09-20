#!/usr/bin/env bash
# Заводит автоматическую выкатку: ключ от контура и два секрета
# репозитория. Гоняется ОДИН раз и с машины, у которой уже есть доступ к
# контуру по ssh, — то есть с машины владельца.
#
# # Почему это не делает сессия
#
# Ключ, которым сторож ходит на контур, порождается здесь и уезжает прямо
# в секреты репозитория, минуя чей-либо экран, разговор и переписку.
# Сессия сделать этого не может и не должна: у неё нет доступа к контуру
# (порт 22 закрыт), а закрытая половина ключа, показанная в чате, уже не
# закрытая.
#
# # Что остаётся после
#
# На контуре — одна строка в authorized_keys для root. В репозитории — два
# секрета: CURATOR_SSH_KEY (закрытая половина) и CURATOR_SSH_KNOWN_HOSTS
# (отпечаток хоста). Закрытая половина на этой машине НЕ остаётся: каталог
# с ней стирается при выходе, как бы скрипт ни кончился.
set -euo pipefail

HOST="${CURATOR_HOST:-root@curator.psync.ru}"
REPO="${CURATOR_REPO:-zazzool/Curator}"

die() {
    printf '%s\n' "$@" >&2
    exit 1
}

command -v ssh-keygen >/dev/null || die "Нет ssh-keygen."
command -v ssh-keyscan >/dev/null || die "Нет ssh-keyscan."
command -v gh >/dev/null ||
    die "Нет gh — утилиты GitHub, которой кладутся секреты." \
        "Поставить: https://cli.github.com, затем gh auth login." \
        "Без неё секреты заводятся руками: Settings → Secrets and variables →" \
        "Actions. Что именно класть, скрипт напечатает и без gh не сможет."

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
echo "Репозиторий: $REPO"
echo

# Каталог стирается при любом выходе, включая Ctrl-C: закрытая половина
# ключа не должна пережить этот скрипт ни на минуту.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

echo "== Порождаю ключ =="
# Без пароля намеренно: пароль у ключа, которым пользуется сторож, значил
# бы, что пароль лежит вторым секретом рядом. Защита ключа здесь — в том,
# что закрытую половину видит только GitHub.
ssh-keygen -t ed25519 -N '' -C "curator-deploy (GitHub Actions)" -f "$WORK/key" >/dev/null
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

echo "== Кладу секреты в $REPO =="
gh secret set CURATOR_SSH_KEY --repo "$REPO" < "$WORK/key"
gh secret set CURATOR_SSH_KNOWN_HOSTS --repo "$REPO" < "$WORK/known_hosts"

cat <<DONE

Готово. Дальше выкатывает сторож: всякое зелёное слияние в main уезжает на
$HOST само.

Толкнуть руками — вкладка Actions → «Выкатка» → Run workflow.
Там же и возврат предыдущего образа, когда контур уже горит.

Отозвать доступ, если ключ понадобится убрать:
    ssh $HOST "sed -i '/curator-deploy (GitHub Actions)/d' ~/.ssh/authorized_keys"
DONE
