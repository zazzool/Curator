# Образ Куратора: один бинарь отдаёт и студию, и API.
#
# Три стадии нужны затем, чтобы в рантайм не уехали ни node_modules, ни
# компилятор Go: итоговый образ — это alpine, бинарь и статика.
#
# Версия Go берётся из go.mod, а не пишется здесь: два места для одной
# версии расходятся молча, и узнают об этом по сборке, которая собралась
# не тем компилятором. Версия Node названа здесь и в сторже — больше
# назвать её негде, и это единственное её второе место, оговорённое вслух.

# --- Студия ---
FROM node:22-alpine AS web
WORKDIR /build

# Зависимости отдельным слоем: package.json меняется реже исходников, и
# пересборка образа не тянет npm ci каждый раз.
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# --- Сервер ---
FROM golang:1.25-alpine AS server
WORKDIR /build

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
# CGO не нужен: драйвер pgx чистый на Go, а статический бинарь избавляет
# рантайм от зависимости на конкретную libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/curator . && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/user ./cmd/user && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/import ./cmd/import

# --- Рантайм ---
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app

WORKDIR /app
COPY --from=server /out/curator /app/curator

# Накат схемы едет вместе с сервером: схему применяют на контуре, а не с
# машины разработчика — база видна только из сети сервера.
COPY --from=server /out/migrate /app/migrate
COPY server/schema.sql /app/schema.sql

# Заведение пользователя студии — там же и потому же. На свежем контуре
# пользователей нет ни одного, а заводит их право workshop: войти, чтобы
# завести первого, нельзя по кругу.
COPY --from=server /out/user /app/user

# Ввоз из прежней системы. Работа разовая, но ходит она в живую базу
# другого продукта, и ходить ей надо из сети сервера, а не из чужого
# терминала.
COPY --from=server /out/import /app/import

COPY --from=web /build/dist /app/web

# Адрес контура задаётся явно, а не оставляется пустым. У переменной есть
# умолчание в коде, и пустое значение включило бы именно его — но
# умолчание должно быть последним доводом, а не тем, на чём стоит боевой
# контур: перееди контур на другое имя, и узнают об этом по ссылкам в
# чужих письмах.
ENV CURATOR_ADDR=:8080 \
    CURATOR_EDITOR_DIR=/app/web \
    CURATOR_PUBLIC_URL=https://curator.psync.ru

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/curator"]
