#!/bin/bash
# Клиент AmneziaWG и SOCKS5-прокси поверх него.
#
# Два правила, из которых следует всё остальное:
#
#  1. Без туннеля прокси не работает. Запустить его «пока без VPN»
#     значило бы, что запросы к поставщику молча пойдут напрямую, и
#     заметить это будет нечем — ответы-то приходят.
#
#  2. Конфигурацию кладёт человек на контуре, а не служба. У донора её
#     приносил редактор через общий том; здесь такого экрана нет, и
#     заводить ради одного файла право, экран и том значило бы строить
#     механизм вокруг действия, которое делается раз в год.
#
# Файл смонтирован только для чтения, и это тоже правило: в нём
# приватный ключ, и служба, способная его переписать, однажды перепишет.
set -uo pipefail

VPN_DIR="${LLM_VPN_DIR:-/vpn}"
SOURCE="$VPN_DIR/awg0.conf"
CONFIG=/tmp/awg0.conf
IFACE=awg0
PROXY_PID=""

# Приложение Amnezia выгружает все параметры обфускации, включая
# незаданные: «I2 = » без значения. Инструменты такую строку не
# принимают и отказываются разбирать файл целиком — то есть исправная
# конфигурация выглядит как негодная.
sanitize() {
    awk '
        /^[[:space:]]*[#;]/ { print; next }
        /^[[:space:]]*\[/   { print; next }
        /^[[:space:]]*$/    { print; next }
        {
            line = $0
            eq = index(line, "=")
            if (eq == 0) { print; next }
            value = substr(line, eq + 1)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
            if (value != "") print
        }
    ' "$SOURCE" > "$CONFIG"
}

tunnel_down() {
    [ -n "$PROXY_PID" ] && kill "$PROXY_PID" 2>/dev/null
    PROXY_PID=""
    awg-quick down "$CONFIG" 2>/dev/null
    # Аварийный выключатель снимаем вместе с туннелем, иначе следующая
    # попытка не сможет достучаться даже до сервера VPN.
    iptables -P OUTPUT ACCEPT 2>/dev/null
    iptables -F OUTPUT 2>/dev/null
}

tunnel_up() {
    export WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go
    export WG_SUDO=1

    sanitize
    if ! awg-quick up "$CONFIG"; then
        echo "интерфейс не поднялся — проверьте конфигурацию $SOURCE" >&2
        return 1
    fi

    # Ждём рукопожатия: интерфейс поднимается мгновенно, а связь нет.
    # Открыть прокси раньше значит принять запрос, который некуда слать.
    local handshake=""
    local attempt
    for attempt in $(seq 1 30); do
        handshake=$(awg show "$IFACE" latest-handshakes 2>/dev/null | awk '{print $2}' | head -1)
        [ -n "$handshake" ] && [ "$handshake" != "0" ] && break
        sleep 1
    done
    if [ -z "$handshake" ] || [ "$handshake" = "0" ]; then
        echo "нет рукопожатия с сервером VPN за 30 секунд — гашу туннель" >&2
        awg-quick down "$CONFIG" 2>/dev/null
        return 1
    fi

    # Наружу можно только через туннель. Отвалится — запросы начнут
    # падать, и это правильное поведение: молча уйти в обход VPN хуже,
    # чем не уйти никуда.
    local local_net="${LLM_VPN_LOCAL_NET:-172.16.0.0/12}"
    iptables -P OUTPUT DROP
    iptables -A OUTPUT -o lo -j ACCEPT
    iptables -A OUTPUT -o "$IFACE" -j ACCEPT
    iptables -A OUTPUT -d "$local_net" -j ACCEPT
    # Сам сервер VPN — исключение, и без него выключатель отрезал бы
    # туннель от его же собственного адресата.
    local endpoint_ip
    endpoint_ip=$(awg show "$IFACE" endpoints | awk '{print $2}' | cut -d: -f1 | head -1)
    [ -n "$endpoint_ip" ] && iptables -A OUTPUT -d "$endpoint_ip" -j ACCEPT

    # В Alpine бинарь dante называется sockd, а не danted.
    sockd -f /etc/danted.conf -N 1 &
    PROXY_PID=$!
    echo "туннель поднят, прокси на 1080"
}

trap 'tunnel_down; exit 0' TERM INT

last_hash=""
while true; do
    if [ ! -f "$SOURCE" ]; then
        if [ -n "$PROXY_PID" ]; then
            echo "конфигурация убрана — гашу туннель" >&2
            tunnel_down
        fi
        echo "конфигурации нет: положите её в $SOURCE (см. deploy/llm-vpn/README.md)" >&2
        sleep 30
        continue
    fi

    current_hash=$(sha256sum "$SOURCE" | awk '{print $1}')
    if [ "$current_hash" != "$last_hash" ]; then
        echo "конфигурация изменилась — перенастраиваю"
        tunnel_down
        last_hash="$current_hash"
        tunnel_up
        sleep 5
        continue
    fi

    # Прокси мог умереть сам: поднимаем заново, но только вместе с
    # туннелем — прокси без туннеля запускать нельзя.
    if [ -n "$PROXY_PID" ] && ! kill -0 "$PROXY_PID" 2>/dev/null; then
        echo "прокси остановился — поднимаю заново" >&2
        tunnel_down
        tunnel_up
        sleep 5
        continue
    fi

    # Туннель мог отвалиться при живом прокси. Гасим и поднимаем заново:
    # прокси, переживший туннель, — это и есть тот случай, ради которого
    # стоит аварийный выключатель, и оставлять его работать незачем.
    if [ -n "$PROXY_PID" ]; then
        handshake=$(awg show "$IFACE" latest-handshakes 2>/dev/null | awk '{print $2}' | head -1)
        if [ -z "$handshake" ] || [ "$handshake" = "0" ]; then
            echo "рукопожатие потеряно — поднимаю туннель заново" >&2
            tunnel_down
            tunnel_up
        fi
    fi
    sleep 10
done
