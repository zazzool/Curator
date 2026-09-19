# Сайдкар: клиент AmneziaWG плюс SOCKS5-прокси.
#
# Через него ходит только клиент поставщика моделей. Раздача задач,
# студия и база работают напрямую: туннель нужен ровно для одного
# адресата, и заворачивать в него весь сервер значило бы усложнить сеть
# там, где этого не просили, — платили бы задержкой и лишней точкой
# отказа все, а польза только у одного.
#
# AmneziaWG собирается из исходников: в пакетах Alpine его нет.

FROM golang:1.25-alpine AS awg
RUN apk add --no-cache git
ARG AWG_GO_REF=master
RUN git clone --depth 1 --branch ${AWG_GO_REF} \
        https://github.com/amnezia-vpn/amneziawg-go.git /src/awg-go && \
    cd /src/awg-go && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/amneziawg-go .

FROM alpine:3.21 AS tools
RUN apk add --no-cache git make gcc musl-dev linux-headers bash
ARG AWG_TOOLS_REF=master
RUN git clone --depth 1 --branch ${AWG_TOOLS_REF} \
        https://github.com/amnezia-vpn/amneziawg-tools.git /src/awg-tools && \
    cd /src/awg-tools/src && make && make DESTDIR=/out install

# awg-quick — обычный bash-скрипт, и его установку Makefile в этой сборке
# пропускает. Кладём сами: разбираться с условиями в чужом Makefile
# дольше, чем скопировать файл.
#
# Заодно правим одну строку: в контейнере /proc/sys только для чтения, и
# скрипт падает, пытаясь выставить src_valid_mark. Значение задаётся
# декларативно при создании контейнера (sysctls в docker-compose), так
# что неудача здесь не должна останавливать подъём туннеля.
#
# grep следом — не украшение: sed, не нашедший строку, молча ничего не
# делает и возвращает успех, и сборка выдала бы образ с непропатченным
# скриптом. Обнаружилось бы это на контуре, при первом подъёме туннеля.
RUN install -Dm755 /src/awg-tools/src/wg-quick/linux.bash /out/usr/bin/awg-quick && \
    sed -i 's|cmd sysctl -q net.ipv4.conf.all.src_valid_mark=1|{ cmd sysctl -q net.ipv4.conf.all.src_valid_mark=1 \|\| true; }|' \
        /out/usr/bin/awg-quick && \
    grep -q 'src_valid_mark=1 || true' /out/usr/bin/awg-quick

FROM alpine:3.21
RUN apk add --no-cache dante-server iproute2 iptables openresolv bash

COPY --from=awg /out/amneziawg-go /usr/bin/amneziawg-go
COPY --from=tools /out/usr/bin/awg /usr/bin/awg
COPY --from=tools /out/usr/bin/awg-quick /usr/bin/awg-quick

COPY danted.conf /etc/danted.conf
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Порт прокси доступен только внутри сети docker: наружу он не
# публикуется.
EXPOSE 1080

# Здоров тот сайдкар, у которого есть рукопожатие. Живой процесс здесь
# ничего не значит: прокси без туннеля — это прокси, который отправит
# запросы в обход.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
    CMD awg show awg0 latest-handshakes | awk '{ exit ($2 == 0) }'

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
