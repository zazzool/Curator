module curator/server

go 1.26.0

// Чем СОБИРАТЬ — отдельной строкой от того, на каком языке написано.
//
// Сторож ставит ровно то, что названо здесь, и до 20.09.2026 названа была
// одна строка «go 1.25.0» — нулевая заплата, вышедшая в ветке первой.
// Первый же прогон govulncheck нашёл в ней ТРИДЦАТЬ известных дыр
// стандартной библиотеки: crypto/tls, crypto/x509, net/url, net/http,
// encoding/asn1, encoding/pem. Закрыты они заплатами 1.25.2–1.25.8, и всё
// это время сборка выглядела исправной — просто никто не смотрел.
//
// Поднимается эта строка работой, а не сама: заплаты выходят раз в месяц с
// лишним. Зато теперь о том, что пора, говорит сторож, а не случай.
toolchain go1.26.8

require github.com/jackc/pgx/v5 v5.11.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
