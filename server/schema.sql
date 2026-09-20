-- Устройство данных Куратора.
--
-- Схема растёт только добавлением: все ALTER идемпотентны, все INSERT — с
-- ON CONFLICT DO NOTHING. Накат применяется до подмены кода, потому что код,
-- вышедший раньше своей схемы, падает не при старте, а на первом обращении
-- к недостающей колонке — то есть у врача, а не в журнале выкатки.
--
-- # Два отличия от MVExams, заложенные здесь, а не добавленные сверху
--
-- 1. **Колонки владельца нет ни у одной таблицы.** Организаций в Кураторе
--    нет: одна установка — один владелец. В MVExams колонку организации
--    несли 35 таблиц из 111, изоляцию держали политики уровня строки, а
--    доступ шёл через шлюз с объявлением организации в каждой транзакции.
--    Всё это здесь отсутствует намеренно: изоляция, которой нечего
--    изолировать, стоит дорого и ошибается молча.
--
-- 2. **У задачи есть ссылка на единицу источника.** В MVExams метка
--    источника лежала строкой в теле задачи, и два приказа с одинаковой
--    нумерацией пунктов были неразличимы, а «раздел» и «рубрика»
--    вычислялись отрезанием знаков от кода МКБ. Здесь связь выражена
--    ключами, а срезы берутся из пути единицы.
--
-- Порядок разделов — порядок сквозного пути: источник, генерация, задачи и
-- раздача, клиенты и продажи, прогресс и знаки, анализ поведения.

-- ===========================================================================
-- 1. ИСТОЧНИКИ
-- ===========================================================================

-- Источник — то, по чему пишутся задачи и чем они поверяются.
--
-- Классификация, приказ, рекомендации, стандарт, руководство. МКБ-10 —
-- обычная строка этой таблицы, а не встроенное знание: в этом весь смысл
-- устройства. Появление в коде условия «если это МКБ» означает, что у
-- источника не спросили его свойство, а угадали по формату метки.
--
-- Четыре свойства ниже взяты не из головы: назначение — из LOM 9.1 purpose,
-- смысл вложенности — из FHIR hierarchyMeaning, полнота — из FHIR content,
-- редакция — из практики версионирования справочников.
CREATE TABLE IF NOT EXISTS sources (
    id              BIGSERIAL   PRIMARY KEY,
    slug            TEXT        NOT NULL UNIQUE CHECK (slug <> ''),

    -- Вид источника. Словарь закрыт: новый вид — это работа, а не значение,
    -- незаметно появившееся в колонке.
    kind            TEXT        NOT NULL CHECK (kind IN
                        ('classification', 'decree', 'guidelines',
                         'standard', 'handbook', 'other')),

    title           TEXT        NOT NULL CHECK (title <> ''),

    -- Словарь интерфейса: как звать единицу и положение у ЭТОГО источника.
    -- Пустым не бывает: интерфейс без слова показал бы «единица» врачу,
    -- который ждёт слова «диагноз» или «пункт».
    unit_word       TEXT        NOT NULL CHECK (unit_word <> ''),
    statement_word  TEXT        NOT NULL CHECK (statement_word <> ''),

    -- По какой оси источник классифицирует. Отсюда подбор задач понимает,
    -- складываются ли два источника в одну ось или стоят поперёк.
    purpose         TEXT        NOT NULL DEFAULT 'topic' CHECK (purpose IN
                        ('topic', 'system', 'discipline', 'task',
                         'level', 'legal', 'other')),

    -- Смысл вложенности объявляется, а не подразумевается: раздел
    -- классификации и пункт приказа вложены по-разному, и считать их
    -- одинаковыми — значит молча обеднять подбор.
    hierarchy       TEXT        NOT NULL DEFAULT 'grouped' CHECK (hierarchy IN
                        ('part-of', 'is-a', 'grouped')),

    -- Полнота — обязательна и честна. Документ, разобранный моделью из
    -- DOCX, это КУСОК, и объявлять его полным нельзя: на полноту опирается
    -- расчёт охвата, и ложная полнота даёт ложные доли.
    completeness    TEXT        NOT NULL DEFAULT 'fragment' CHECK (completeness IN
                        ('complete', 'fragment')),

    edition         TEXT        NOT NULL DEFAULT '',
    issued          TEXT        NOT NULL DEFAULT '',
    url             TEXT        NOT NULL DEFAULT '',
    legal_note      TEXT        NOT NULL DEFAULT '',

    -- Умолчание — черновик: источник, о котором не сказано ничего, работать
    -- не должен.
    status          TEXT        NOT NULL DEFAULT 'draft'
                                CHECK (status IN ('draft', 'active', 'retired')),

    -- Свойства, которых у других источников не будет. JSONB, а не колонки:
    -- это данные одного источника, а не поля всех источников на свете.
    profile         JSONB       NOT NULL DEFAULT '{}'
                                CHECK (profile <> 'null'::jsonb),

    -- Выпуск справочника: по нему устройство понимает, что лежащая у него
    -- копия отстала. Меняется приёмкой разбора и ввозом — то есть всем,
    -- что правит единицы и положения.
    --
    -- Число, а не отметка времени: часы у двух машин расходятся, а
    -- «больше» и «меньше» у числа не зависят ни от чьих часов. Сравнение
    -- при этом на равенство, а не на «новее»: копия, отставшая или
    -- забежавшая вперёд, одинаково подлежит замене.
    reference_version INTEGER   NOT NULL DEFAULT 1,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Единица источника: диагноз, пункт приказа, раздел рекомендаций.
--
-- kind разделяет два рода записей: группа (вход в навигацию) и запись, по
-- которой можно спрашивать. Группа — тоже единица, только к ответу не
-- пригодная: без групп список в девятьсот строк остаётся без входа.
--
-- # Путь вместо разбора метки
--
-- path — материализованная цепочка предков ('F/F3/F32/F32.1'), depth — её
-- длина. Считаются при загрузке источника из связи с родителем, а не из
-- формата метки. Это и есть замена отрезанию знаков от кода: «раздел» и
-- «рубрика» становятся срезом пути, и для МКБ результат тот же, но получен
-- данными, а не догадкой о формате.
--
-- Разделитель '/' выбран потому, что в метках справочников он не
-- встречается; метка с разделителем внутри — отказ загрузки, а не молча
-- испорченный путь.
CREATE TABLE IF NOT EXISTS source_units (
    id           BIGSERIAL PRIMARY KEY,
    source_id    BIGINT    NOT NULL REFERENCES sources (id),
    kind         TEXT      NOT NULL DEFAULT 'entry'
                           CHECK (kind IN ('group', 'entry')),
    label        TEXT      NOT NULL CHECK (label <> '' AND label NOT LIKE '%/%'),
    parent_label TEXT      NOT NULL DEFAULT '',
    title        TEXT      NOT NULL,
    path         TEXT      NOT NULL DEFAULT '',
    depth        INTEGER   NOT NULL DEFAULT 0 CHECK (depth >= 0),

    -- Пригодна ли единица к ответу. У групп — нет.
    answerable   BOOLEAN   NOT NULL DEFAULT TRUE,

    -- Место в выгрузке. Порядок — часть проводного формата де-факто: ответ
    -- из базы обязан совпадать с ответом из файла байт в байт, а без
    -- записанного порядка он совпадал бы с точностью до перестановки, то
    -- есть не совпадал бы.
    ord          INTEGER   NOT NULL DEFAULT 0,
    attrs        JSONB     NOT NULL DEFAULT '{}'
                           CHECK (attrs <> 'null'::jsonb)
);

-- Две единицы под одной меткой — два источника правды об одном понятии.
-- Род в ключ не входит: с ним «F00» как группа и «F00» как запись
-- уживались бы в одной таблице, и чтение по метке отдавало бы то одну, то
-- другую. Ключ и стоял так — (source_id, kind, label), — вопреки этому же
-- доводу, написанному рядом с ним.
--
-- Указателем, а не UNIQUE внутри CREATE TABLE, и это не украшение:
-- объявление внутри таблицы ставится только на пустой базе, а накат
-- догоняет колонки, но не ключи. Пара «снять прежний — поставить нынешний»
-- идемпотентна и отрабатывает и на чистой базе, и на живой.
ALTER TABLE source_units DROP CONSTRAINT IF EXISTS source_units_source_id_kind_label_key;
CREATE UNIQUE INDEX IF NOT EXISTS source_units_source_label_key
    ON source_units (source_id, label);
CREATE INDEX IF NOT EXISTS idx_source_units_order
    ON source_units (source_id, kind, ord);

-- Срез по пути — самый частый запрос подбора («всё, что под F3»), и без
-- указателя он читает таблицу целиком.
CREATE INDEX IF NOT EXISTS idx_source_units_path
    ON source_units (source_id, path text_pattern_ops);

-- Положение единицы: текст, на который ссылается разметка задачи.
--
-- Единица названа меткой, а не ссылкой на строку единицы, намеренно: блок
-- положений может относиться к метке, которая живёт на положениях предка
-- или отсутствует в перечне единиц выгрузки, и жёсткий ключ ронял бы
-- загрузку на исправных данных.
CREATE TABLE IF NOT EXISTS source_unit_statements (
    id          BIGSERIAL PRIMARY KEY,
    source_id   BIGINT    NOT NULL REFERENCES sources (id),
    unit_label  TEXT      NOT NULL CHECK (unit_label <> ''),
    kind        TEXT      NOT NULL DEFAULT '',

    -- Обозначение в первоисточнике ('G1', 'А', 'абз. 2') и ссылка на место
    -- (страница, пункт). У выгрузки, где их нет, сеются пустыми, а не
    -- выдуманными: выдуманная ссылка на место хуже отсутствующей.
    designation TEXT      NOT NULL DEFAULT '',
    place_ref   TEXT      NOT NULL DEFAULT '',

    body_md     TEXT      NOT NULL,
    ord         INTEGER   NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_source_unit_statements_unit
    ON source_unit_statements (source_id, unit_label, ord);

-- Пары «путают с» и таблица различий.
--
-- Данные источника, а не платформы: это единственное место, где сходство
-- двух единиц названо прямо, и подбор неверных вариантов опирается на них.
-- У источника без такой разметки таблица просто пуста.
CREATE TABLE IF NOT EXISTS source_unit_differentials (
    id           BIGSERIAL PRIMARY KEY,
    source_id    BIGINT    NOT NULL REFERENCES sources (id),
    unit_label   TEXT      NOT NULL CHECK (unit_label <> ''),
    counterpart  TEXT      NOT NULL CHECK (counterpart <> ''),
    features_md  TEXT      NOT NULL,
    ord          INTEGER   NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_source_unit_differentials_order
    ON source_unit_differentials (source_id, ord);

-- Карантин: почему по этой единице нельзя писать задачи.
--
-- Редакционная надстройка над выгрузкой, а не её часть, — поэтому отдельная
-- таблица, а не поле единицы: сама выгрузка остаётся точной копией того,
-- что отдал источник. Пусто — всё исправно.
CREATE TABLE IF NOT EXISTS source_unit_quarantine (
    id          BIGSERIAL PRIMARY KEY,
    source_id   BIGINT    NOT NULL REFERENCES sources (id),
    unit_label  TEXT      NOT NULL CHECK (unit_label <> ''),
    reason      TEXT      NOT NULL CHECK (reason <> ''),
    verified_at TIMESTAMPTZ NULL,
    -- Два карантина одной единицы — две причины, из которых показана будет
    -- случайная.
    UNIQUE (source_id, unit_label)
);

-- Загруженный документ: то, что принесли на разбор.
--
-- Тело хранится как есть, отдельно от разобранного. Разбор можно
-- переделать, документ — нет: он приходит один раз и от него зависит всё
-- остальное.
CREATE TABLE IF NOT EXISTS source_documents (
    id           BIGSERIAL   PRIMARY KEY,
    source_id    BIGINT      NULL REFERENCES sources (id),
    filename     TEXT        NOT NULL CHECK (filename <> ''),
    -- Принимаются текст, Markdown и DOCX. PDF не принимается — это выбранная
    -- граница, а не очередь работ: перевод PDF в текст делает служба
    -- снаружи.
    mime         TEXT        NOT NULL CHECK (mime <> ''),
    byte_size    BIGINT      NOT NULL CHECK (byte_size > 0),
    sha256       TEXT        NOT NULL CHECK (sha256 <> ''),
    body         BYTEA       NOT NULL,
    uploaded_by  TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
    -- Один и тот же файл, принесённый дважды в ОДИН источник, — один
    -- документ. Иначе разбор пойдёт по обеим копиям и даст две редакции
    -- одного источника.
    --
    -- Совпадение считается внутри источника, а не по всей базе, и это
    -- куплено отказом: приказ, положенный во второй источник, возвращал
    -- документ первого — вместе с его источником. Дальше всё шло мимо:
    -- куски переписывались у чужого документа, а «принять разбор»
    -- принимало его в тот источник, которого составитель не открывал.
    -- Видно этого не было ничем: ответ был успешным.
);

-- Прежнее совпадение по одному отпечатку убирается, если оно ещё стоит.
--
-- Снятие ограничения, а не добавление: код, написанный до него, от этого
-- не ломается — прежде запрещённое теперь разрешено, и только. Оставить
-- его рядом с новым нельзя: вставка с ON CONFLICT (source_id, sha256)
-- налетела бы на него и отказала бы с кодом нарушения, а не дала бы
-- второму источнику свой документ.
ALTER TABLE source_documents DROP CONSTRAINT IF EXISTS source_documents_sha256_key;

-- Само совпадение — отдельным указателем, а не ограничением в объявлении
-- таблицы: CREATE TABLE IF NOT EXISTS растит только пустую базу, и на
-- существующей ограничение из объявления не появилось бы вовсе. Указатель
-- же кладётся идемпотентно, и обе базы приходят к одному.
--
-- COALESCE, а не голая колонка: источник у документа может быть не
-- назван, а два NULL в указателе считаются разными — и документ без
-- источника, принесённый дважды, лёг бы двумя. Ноль вместо NULL делает
-- «без источника» таким же местом, как всякое другое.
CREATE UNIQUE INDEX IF NOT EXISTS idx_source_documents_source_sha
    ON source_documents (COALESCE(source_id, 0), sha256);

-- Секции первоисточника: текст, по которому идёт сверка «условие против
-- документа».
--
-- Опора сверки: без неё модель находит противоречия внутри блока, но не
-- находит подмены — а подмена как раз то, чем справочник опасен.
CREATE TABLE IF NOT EXISTS source_sections (
    id          BIGSERIAL PRIMARY KEY,
    source_id   BIGINT    NOT NULL REFERENCES sources (id),
    document_id BIGINT    NULL REFERENCES source_documents (id),
    anchor      TEXT      NOT NULL CHECK (anchor <> ''),
    title       TEXT      NOT NULL DEFAULT '',
    body_md     TEXT      NOT NULL,
    ord         INTEGER   NOT NULL DEFAULT 0,
    UNIQUE (source_id, anchor)
);

-- Куски документа, на которые его порезал разбор.
--
-- Хранятся отдельно от секций, потому что это разные вещи: кусок — единица
-- работы модели, секция — единица первоисточника. Совпадают они у хорошо
-- размеченного документа и расходятся у всех остальных.
CREATE TABLE IF NOT EXISTS source_fragments (
    id          BIGSERIAL PRIMARY KEY,
    document_id BIGINT    NOT NULL REFERENCES source_documents (id),
    ord         INTEGER   NOT NULL DEFAULT 0,
    body_md     TEXT      NOT NULL,
    char_from   INTEGER   NOT NULL DEFAULT 0,
    char_to     INTEGER   NOT NULL DEFAULT 0,
    UNIQUE (document_id, ord)
);

-- Черновик разбора: что модель вынула из документа, пока человек не принял.
--
-- Отдельные таблицы, а не поле «принято» у настоящих единиц: непринятый
-- разбор не должен попадать ни в один запрос, который читает источник.
-- Флаг забывают в условии, отдельную таблицу забыть нельзя.
CREATE TABLE IF NOT EXISTS source_draft_units (
    id           BIGSERIAL PRIMARY KEY,
    document_id  BIGINT    NOT NULL REFERENCES source_documents (id),
    label        TEXT      NOT NULL CHECK (label <> ''),
    parent_label TEXT      NOT NULL DEFAULT '',
    title        TEXT      NOT NULL,

    -- Род записи доезжает до приёмки через черновик. Без этой колонки
    -- приёмка ставила всем 'entry', и групп не бывало вовсе — а без них
    -- справочник в девятьсот строк остаётся без входа: ровно та поломка,
    -- ради которой род и заведён.
    kind         TEXT      NOT NULL DEFAULT 'entry'
                           CHECK (kind IN ('group', 'entry')),

    ord          INTEGER   NOT NULL DEFAULT 0,
    attrs        JSONB     NOT NULL DEFAULT '{}' CHECK (attrs <> 'null'::jsonb),
    UNIQUE (document_id, label)
);

CREATE TABLE IF NOT EXISTS source_draft_statements (
    id          BIGSERIAL PRIMARY KEY,
    document_id BIGINT    NOT NULL REFERENCES source_documents (id),
    unit_label  TEXT      NOT NULL CHECK (unit_label <> ''),
    kind        TEXT      NOT NULL DEFAULT '',
    designation TEXT      NOT NULL DEFAULT '',
    body_md     TEXT      NOT NULL,
    place_ref   TEXT      NOT NULL DEFAULT '',
    ord         INTEGER   NOT NULL DEFAULT 0
);

-- Приёмка разбора: кто и когда перевёл черновик в источник.
--
-- Записывается решение, а не факт нажатия: у приёмки есть автор, и в споре
-- о том, откуда в источнике взялся пункт, отвечать будет он.
CREATE TABLE IF NOT EXISTS source_item_acceptances (
    id          BIGSERIAL   PRIMARY KEY,
    document_id BIGINT      NOT NULL REFERENCES source_documents (id),
    unit_label  TEXT        NOT NULL CHECK (unit_label <> ''),
    decision    TEXT        NOT NULL CHECK (decision IN ('accept', 'reject')),
    note        TEXT        NOT NULL DEFAULT '',
    decided_by  TEXT        NOT NULL DEFAULT '',
    decided_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Врачебная выверка положений.
--
-- Проверить справочник на истинность автоматически нельзя: официального
-- текста у проекта нет, а доступные проверки находят лишь противоречия.
-- Блок может быть безупречно согласован и всё равно неверен.
--
-- Поэтому умолчание перевёрнуто: невыверенные положения не запрещают
-- работать, но запрещают публиковать. Составлять и править задачу можно
-- всегда — до устройств доходит только то, за что врач поручился.
--
-- content_hash — отпечаток текста на момент подписи. Изменились положения —
-- подпись перестаёт действовать сама, и единица снова закрыта. Без этого
-- одна давняя подпись покрывала бы любой будущий текст.
CREATE TABLE IF NOT EXISTS statement_signatures (
    id           BIGSERIAL   PRIMARY KEY,
    source_id    BIGINT      NOT NULL REFERENCES sources (id),
    unit_label   TEXT        NOT NULL CHECK (unit_label <> ''),
    content_hash TEXT        NOT NULL CHECK (content_hash <> ''),
    signed_by    TEXT        NOT NULL CHECK (signed_by <> ''),
    signed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    note         TEXT        NOT NULL DEFAULT '',
    UNIQUE (source_id, unit_label, content_hash)
);

-- ===========================================================================
-- 2. ГЕНЕРАЦИЯ
-- ===========================================================================

-- Промпты генерации.
--
-- Формулировка задания модели — предмет врачебной работы, а не деталь
-- реализации: от неё зависит, получится ли достоверная виньетка. Поэтому
-- промпты правятся в студии, а не в коде.
CREATE TABLE IF NOT EXISTS prompts (
    id         TEXT        PRIMARY KEY,
    name       TEXT        NOT NULL,
    -- Узел конвейера, которому принадлежит промпт: написание условия,
    -- подбор неверных вариантов, критик, сверка, вычитка.
    node       TEXT        NOT NULL DEFAULT 'compose',
    system_md  TEXT        NOT NULL,
    user_md    TEXT        NOT NULL,
    -- Модель ЭТОГО узла. Пусто — модель поставщика по умолчанию.
    --
    -- Узлы стоят разных денег и требуют разного: написание условия — самая
    -- дорогая работа конвейера, а слепая сверка отвечает одним словом из
    -- списка, и платить за неё по цене написания незачем. Одна модель на
    -- весь конвейер означала выбор между «дорого везде» и «плохо везде».
    --
    -- Пусто, а не имя модели умолчанием: записанное имя устареет молча.
    -- Поставщик сменит рекомендованную модель, а у нас останется прежняя,
    -- вписанная накатом полгода назад, и выглядеть это будет как выбор.
    model      TEXT        NOT NULL DEFAULT '',
    is_default BOOLEAN     NOT NULL DEFAULT FALSE,
    revision   INTEGER     NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Промпт по умолчанию ровно один на узел: двух генерация не разберёт.
CREATE UNIQUE INDEX IF NOT EXISTS idx_prompts_default
    ON prompts (node) WHERE is_default;

-- Свод правил: то, чего система держится, когда пишет задачу. Это и есть
-- механизм самообучения конвейера — то, что приходилось править руками,
-- становится правилом и уходит в задание следующей задачи.
--
-- Правило НЕ УДАЛЯЕТСЯ, а закрывается датой (valid_to): свод дрейфует, и
-- дрейф замечают через недели, когда вопрос «чего мы требовали в марте»
-- уже некому задать. Поэтому здесь нет и не будет DELETE.
--
-- Имя «rulebook», а не «rules», потому что таблица `rules` в этой схеме
-- уже объявлена (ниже, про негодные задачи) и не используется ни одной
-- строкой кода. Второе объявление того же имени `CREATE TABLE IF NOT
-- EXISTS` пропустил бы МОЛЧА — на живой базе свод просто не завёлся бы,
-- а догоняющий накат принялся бы дописывать его колонки в чужую
-- таблицу. Мёртвое объявление здесь не трогается: оно вне этой работы.
CREATE TABLE IF NOT EXISTS rulebook (
    id            TEXT        PRIMARY KEY,
    title         TEXT        NOT NULL,
    text          TEXT        NOT NULL,
    why           TEXT        NOT NULL DEFAULT '',
    -- Род правила: существо, согласованность, разметка, устройство, слог.
    -- Определяет вес при отборе в задание: место в блоке ограничено, и
    -- жертвовать надо правилом о слоге, а не о существе.
    kind          TEXT        NOT NULL,
    -- Откуда правило взялось: составитель, встроенное, правка, замечание
    -- детектора. От источника зависит старшинство при споре правил.
    source        TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'candidate',
    -- Состояние назначено человеком, а не счётчиком подтверждений.
    -- Без этого признака включение правила рукой молча не работает:
    -- пересчёт по кворуму возвращает «кандидат», отвечая при этом успехом.
    pinned        BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Область действия: источники, метки единиц, виды задачи, узлы.
    -- Пустой объект — «всегда». Ни одного измерения, знающего один
    -- источник: источник здесь любой.
    scope         JSONB       NOT NULL DEFAULT '{}'::jsonb
                  CHECK (jsonb_typeof(scope) = 'object'),
    -- Машинная проверка правила: предикат из закрытого каталога и его
    -- параметры. NULL — правило проверяется только заданием модели.
    -- Каталог живёт в коде (internal/rules/check.go), а не в схеме:
    -- предикат без кода, который его исполняет, молча не срабатывает.
    check_json    JSONB       NULL
                  CHECK (check_json IS NULL OR jsonb_typeof(check_json) = 'object'),
    confirmations INTEGER     NOT NULL DEFAULT 0,
    -- Задания, уже подтвердившие правило. Отдельно от примеров: примеры
    -- обрезаются, и обрезка стирала бы память о подтвердивших заданиях —
    -- задание, чей пример вытеснили, подтвердило бы правило снова.
    seen_jobs     BIGINT[]    NOT NULL DEFAULT '{}',
    -- В какое правило это слито при уплотнении свода. NULL у всех
    -- прочих. Ссылка, а не просто состояние «слито»: без адреса
    -- слияние неотличимо от пропажи, и вопрос «куда делось моё
    -- правило» остался бы без ответа. По ней же подтверждения петли
    -- самообучения доезжают до выжившего, а не копятся у закрытого.
    -- Без REFERENCES намеренно: свод растят накатом и правкой, и
    -- ссылка на ещё не заведённое правило уронила бы весь накат вместо
    -- того, чтобы оставить одну повисшую ссылку.
    merged_into   TEXT        NULL,
    valid_from    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_to      TIMESTAMPTZ NULL,
    last_seen_at  TIMESTAMPTZ NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- История правок промпта: сравнить «до» и «после» нужно ровно тогда, когда
-- качество задач поехало, а помнить, что именно меняли неделю назад, уже
-- некому.
CREATE TABLE IF NOT EXISTS prompt_revisions (
    prompt_id  TEXT        NOT NULL REFERENCES prompts (id),
    revision   INTEGER     NOT NULL,
    system_md  TEXT        NOT NULL,
    user_md    TEXT        NOT NULL,
    saved_by   TEXT        NOT NULL DEFAULT '',
    saved_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (prompt_id, revision)
);

-- Какая модель стоит на каком узле конвейера.
--
-- Настройкой в базе, а не в сборке: цены и доступность моделей меняются
-- чаще, чем выходит сборка, и переезд узла на другую модель не должен
-- требовать выкатки.
CREATE TABLE IF NOT EXISTS node_models (
    node       TEXT        PRIMARY KEY,
    model      TEXT        NOT NULL CHECK (model <> ''),
    provider   TEXT        NOT NULL DEFAULT '',
    params     JSONB       NOT NULL DEFAULT '{}' CHECK (params <> 'null'::jsonb),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Задание на генерацию и его состояние.
--
-- Задание живёт отдельно от задачи, которую оно породило: задание — это
-- сведения о том, КАК задачу получили, а замечания и текст — сведения о ней
-- самой. Смешать их значит потерять и то и другое при первой же перегенерации.
CREATE TABLE IF NOT EXISTS gen_jobs (
    id          BIGSERIAL   PRIMARY KEY,
    source_id   BIGINT      NOT NULL REFERENCES sources (id),
    unit_label  TEXT        NOT NULL DEFAULT '',

    -- Род работы: написать задачу ('case') или разобрать документ ('parse').
    --
    -- Одна очередь на оба рода, а не две таблицы: и там и там работа — это
    -- обращение к модели с заданием из той же таблицы, с тем же учётом и с
    -- тем же отказом. Вторая очередь означала бы второй отбор, второй
    -- сторож брошенных и второй лоток в студии, и расходились бы они молча.
    --
    -- Но исполнители у родов РАЗНЫЕ, и отбор поэтому идёт по роду:
    -- исполнитель, взявший чужое задание, прочёл бы чужой заказ и выполнил
    -- не то. Род при отборе — условие в WHERE, а не подпись в списке.
    --
    -- Умолчание 'case' означает буквально «прежние задания — это задачи»:
    -- колонка доезжает до живой базы догоняющим накатом, и задания,
    -- заведённые до неё, обязаны остаться тем, чем были.
    kind        TEXT        NOT NULL DEFAULT 'case'
                            CHECK (kind IN ('case', 'parse')),
    status      TEXT        NOT NULL DEFAULT 'queued'
                            CHECK (status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    step        TEXT        NOT NULL DEFAULT '',
    attempts    INTEGER     NOT NULL DEFAULT 0,

    -- Замечания по ходу работы: что отброшено, какая часть не разобралась.
    --
    -- Отдельно от error, и это не дробность. error — причина, по которой
    -- работа НЕ СДЕЛАНА, и она одна. Замечание говорит о сделанной работе:
    -- одна часть документа из восьмидесяти не далась, из ответа модели
    -- отброшено четыре единицы. Свали их в одно поле — и задание с одной
    -- неудавшейся частью читалось бы как провалившееся целиком, а
    -- семьдесят девять разобранных частей пропали бы из виду.
    notes       JSONB       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(notes) = 'array'),

    -- Ключ повторности: пока задание ИДЁТ, повтор с тем же ключом не
    -- заводит второго. Держит это указатель базы, а не проверка перед
    -- вставкой: проверка и вставка — два шага, и соперники сталкиваются
    -- между ними. Почему именно идущее, а не всякое, — у указателя ниже.
    idem_key    TEXT        NOT NULL DEFAULT '',
    params      JSONB       NOT NULL DEFAULT '{}' CHECK (params <> 'null'::jsonb),
    error       TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Ключ повторности держит только ИДУЩУЮ работу, а не всю историю.
--
-- Прежний указатель был по всем заданиям с ключом, и это запирало единицу
-- навсегда. Задание отказало — строка с ключом осталась, и повторный заказ
-- той же единицы молча возвращал прежнее, закрытое задание вместо нового.
-- Составителю это показывалось как «нажал и ничего не произошло», а
-- единица выбывала из работы насовсем: отменить её было нечем, потому что
-- отменять нечего — задание давно закрыто.
--
-- Защита от двойного нажатия при этом цела и осталась там же, где была: в
-- указателе, а не в проверке перед вставкой. Проверка и вставка — два
-- шага, и соперники сталкиваются между ними.
--
-- Прежний указатель снимается здесь же и до заведения нового: два
-- указателя по одному ключу означали бы, что старый по-прежнему запирает,
-- а новый ничего не решает. DROP идёт первым заходом наката, CREATE —
-- третьим (см. cmd/migrate), так что порядок держится сам.
DROP INDEX IF EXISTS idx_gen_jobs_idem;
CREATE UNIQUE INDEX IF NOT EXISTS idx_gen_jobs_idem_live
    ON gen_jobs (idem_key)
    WHERE idem_key <> '' AND status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS idx_gen_jobs_queue
    ON gen_jobs (status, id) WHERE status IN ('queued', 'running');

-- Черновик задачи до того, как она стала задачей.
CREATE TABLE IF NOT EXISTS case_drafts (
    id         BIGSERIAL   PRIMARY KEY,
    job_id     BIGINT      NULL REFERENCES gen_jobs (id),
    source_id  BIGINT      NOT NULL REFERENCES sources (id),
    unit_label TEXT        NOT NULL DEFAULT '',
    body       JSONB       NOT NULL,

    -- Что известно о слепой сверке этого черновика.
    --
    -- До этой колонки вердикт сверки вычислялся и ПРОПАДАЛ: конвейер
    -- отдавал его исполнителю, тот писал в журнал «задание написано» и
    -- терял. То есть за сверку платили, а составитель её не видел — и
    -- задача, с которой сверка не согласилась, выглядела точно так же,
    -- как та, с которой согласилась.
    --
    -- NULL значит «сверки не было», и это не то же самое, что несогласие.
    -- Несогласие — это работа: посмотреть и решить. Несостоявшаяся сверка
    -- означает, что задачу не проверял никто, и различать их обязательно:
    -- слитые в одно, они дали бы «всё чисто» у сотни неproверенных задач
    -- подряд. Поэтому же внутри держится и причина, по которой сверка не
    -- состоялась: кончились деньги у поставщика, отменили задание и модель
    -- вернула не тот JSON — на глаз одно и то же, а делать надо разное.
    blind_check JSONB      NULL,

    -- Итоги различающей сверки: подтверждает ли условие ещё и неверный
    -- вариант.
    --
    -- Своей колонкой рядом со слепой сверкой, а не внутри неё: вопросы
    -- разные. Слепая отвечает «ведёт ли условие к заказанному ответу» и
    -- отвечает «да» в том числе у задачи, где условие точно так же ведёт
    -- к соседу. Второй верный ответ она пропускает насквозь, и находит
    -- его потом врач — решив задачу правильно и получив «неверно».
    --
    -- NULL значит «не сверялись», пустой список — «сверять было нечего»
    -- (у задачи-действия единиц за вариантами нет вовсе). Это разные
    -- случаи, и сливать их нельзя по тому же доводу, что у blind_check.
    sibling_checks JSONB   NULL
                   CHECK (sibling_checks IS NULL
                          OR jsonb_typeof(sibling_checks) = 'array'),

    -- Итог вычитки: что правлено редактором и что отклонено заслоном.
    -- NULL значит «вычитки не было вовсе» — узел не дошёл; заполненное с
    -- done: false значит «попытка была и не удалась». Разные вещи:
    -- второе называет причину, а первое означает, что язык задачи не
    -- смотрел никто.
    proofread      JSONB   NULL
                   CHECK (proofread IS NULL
                          OR jsonb_typeof(proofread) = 'object'),

    -- Итог детектора подсказок: не называет ли условие ответ прямо.
    -- NULL — детектор не дошёл; done: false — дошёл и судить не смог
    -- (словаря источника не набралось). Второе не то же, что «чисто»:
    -- прими одно за другое, и источник без словаря объявил бы чистым
    -- весь набор.
    cue_check      JSONB   NULL
                   CHECK (cue_check IS NULL
                          OR jsonb_typeof(cue_check) = 'object'),

    -- Итог судьи: машинные проверки свода по готовому черновику.
    -- NULL — судья не ходил; done: false — ходил и судить не смог
    -- (свод не прочитан). Разница та же, что у детектора: задача,
    -- которую никто не судил, не есть задача без нарушений.
    rule_check     JSONB   NULL
                   CHECK (rule_check IS NULL
                          OR jsonb_typeof(rule_check) = 'object'),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Разбор условия на фрагменты: что именно в тексте подтверждает положение.
--
-- Разметка — половина ценности задачи: без неё обучающийся видит вердикт,
-- но не видит, чем он обоснован.
CREATE TABLE IF NOT EXISTS case_chunks (
    id         BIGSERIAL PRIMARY KEY,
    case_id    TEXT      NOT NULL,
    ord        INTEGER   NOT NULL DEFAULT 0,
    text       TEXT      NOT NULL,
    -- На какое положение ссылается фрагмент. Пусто — фрагмент не
    -- подтверждает ничего и это законно: условие содержит и фон.
    statement_id BIGINT  NULL REFERENCES source_unit_statements (id),
    UNIQUE (case_id, ord)
);

-- Наборы неверных вариантов и чем они обоснованы.
--
-- Неверный вариант не берётся с потолка: он опирается либо на пару
-- различий источника, либо на названную стратегию. Вариант без обоснования
-- невозможно ни проверить, ни улучшить.
CREATE TABLE IF NOT EXISTS distractor_sets (
    id         BIGSERIAL   PRIMARY KEY,
    case_id    TEXT        NOT NULL,
    strategy   TEXT        NOT NULL DEFAULT '',
    body       JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Правила конвейера: что считается негодной задачей.
--
-- Правила — данные, а не код, потому что их пишет и правит составитель,
-- глядя на выход конвейера, а не программист при выкатке.
CREATE TABLE IF NOT EXISTS rules (
    id          BIGSERIAL   PRIMARY KEY,
    slug        TEXT        NOT NULL UNIQUE CHECK (slug <> ''),
    title       TEXT        NOT NULL CHECK (title <> ''),
    body_md     TEXT        NOT NULL,
    -- Где правило применяется: ко всем источникам или к одному.
    source_id   BIGINT      NULL REFERENCES sources (id),
    severity    TEXT        NOT NULL DEFAULT 'block'
                            CHECK (severity IN ('block', 'warn')),
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    revision    INTEGER     NOT NULL DEFAULT 1,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Замечания проверок к задаче.
--
-- Живут при задаче, а не при задании, которым она написана: читать их будут,
-- открыв задачу из списка через неделю.
CREATE TABLE IF NOT EXISTS case_findings (
    id         BIGSERIAL   PRIMARY KEY,
    case_id    TEXT        NOT NULL,
    rule_slug  TEXT        NOT NULL DEFAULT '',
    kind       TEXT        NOT NULL DEFAULT 'lint',
    severity   TEXT        NOT NULL DEFAULT 'warn'
                           CHECK (severity IN ('block', 'warn', 'note')),
    body_md    TEXT        NOT NULL,
    -- Судьба замечания: принято, отклонено, ещё не решено. Отклонённое не
    -- удаляется — иначе следующая проверка выкатит его снова, и так по кругу.
    fate       TEXT        NOT NULL DEFAULT 'open'
                           CHECK (fate IN ('open', 'accepted', 'declined')),
    fate_note  TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_findings_case ON case_findings (case_id);

-- Обращения к моделям: что спросили, что ответили, сколько это стоило.
--
-- Записывается каждое обращение, а не сводка: сводку можно посчитать из
-- строк, строки из сводки — нет. Именно по ним считается себестоимость
-- задачи, и именно они нужны, когда модель начала отвечать хуже.
--
-- # Расход считается в долларах, а продажи — в рублях
--
-- Прежде здесь стояли копейки, и это была ошибка, замеченная при переносе
-- двери к моделям: поставщики называют цену в долларах, и перевод её в
-- рубли при записи вбил бы в строку курс того дня. Сумма за месяц после
-- этого не сходится ни с чьим счётом: ни с долларовым счётом поставщика,
-- ни с рублёвым нашим, — а починить её нельзя, потому что курс записи
-- потерян.
--
-- Поэтому две валюты не смешиваются нигде: расход на модели — доллары,
-- выручка и цены пакетов — рубли. Перевод делается при показе, по курсу
-- на день показа, и остаётся видимым как перевод.
--
-- Единица — нанодоллар (10⁻⁹ USD), целым числом: деньги дробным числом не
-- хранятся никогда, а цена одного токена — это миллионные доли доллара, и
-- в копейках она округлилась бы в ноль.
CREATE TABLE IF NOT EXISTS llm_calls (
    id             BIGSERIAL   PRIMARY KEY,
    job_id         BIGINT      NULL REFERENCES gen_jobs (id),
    node           TEXT        NOT NULL DEFAULT '',
    provider       TEXT        NOT NULL DEFAULT '',
    model          TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'ok',
    prompt_tokens  BIGINT      NOT NULL DEFAULT 0,
    output_tokens  BIGINT      NOT NULL DEFAULT 0,
    cached_tokens  BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT  NOT NULL DEFAULT 0,

    cost_nano_usd  BIGINT      NOT NULL DEFAULT 0,

    -- Назвал ли цену поставщик. Расчётная цена не знает ни скидки
    -- префиксного кэша, ни наценки шлюза и ошибается в разы там, где кэш
    -- работает. Сложить её с названной в одну колонку без пометки значит
    -- выдать оценку за факт.
    cost_exact     BOOLEAN     NOT NULL DEFAULT FALSE,

    latency_ms     BIGINT      NOT NULL DEFAULT 0,
    request        JSONB       NULL,
    response       JSONB       NULL,
    error          TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_llm_calls_time ON llm_calls (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_calls_job  ON llm_calls (job_id);

-- Прайс моделей: сколько стоит один токен у поставщика, в нанодолларах.
--
-- В базе, а не в коде: цены меняются у поставщика, а не у нас, и сборка
-- ради новой цены не выпускается.
--
-- Прайс — запасной путь для тех, кто цену не называет. Точную цену
-- обращения называет маршрутизатор, и только он может назвать её честно:
-- внутри него один и тот же запрос уходит разным поставщикам по разным
-- ценам. Посчитанное по прайсу помечается как оценка (cost_exact = FALSE
-- в llm_calls) и в сводке показывается отдельно.
--
-- За токен, а не за тысячу: тысяча — привычная единица прайс-листов, но
-- она заставляет делить при каждом расчёте, и однажды разделят не там.
CREATE TABLE IF NOT EXISTS model_prices (
    provider             TEXT        NOT NULL,
    model                TEXT        NOT NULL,
    prompt_nano_usd      BIGINT      NOT NULL DEFAULT 0,
    completion_nano_usd  BIGINT      NOT NULL DEFAULT 0,

    -- Цена токена, прочитанного из префиксного кэша и записанного в него.
    -- Ноль означает «поставщик не сказал»: тогда кэшированный токен
    -- считается по обычной цене входа, то есть расчёт завышает — и это
    -- правильная сторона для ошибки в оценке.
    cache_read_nano_usd  BIGINT      NOT NULL DEFAULT 0,
    cache_write_nano_usd BIGINT      NOT NULL DEFAULT 0,

    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, model)
);

-- Наборы требований к задаче: что должно быть в условии, чего быть не должно.
CREATE TABLE IF NOT EXISTS instruction_sets (
    id         BIGSERIAL   PRIMARY KEY,
    slug       TEXT        NOT NULL UNIQUE CHECK (slug <> ''),
    title      TEXT        NOT NULL CHECK (title <> ''),
    body_md    TEXT        NOT NULL,
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===========================================================================
-- 3. ЗАДАЧИ И РАЗДАЧА
-- ===========================================================================

-- Задача.
--
-- id текстовый и совпадает с id в пакете: пакет собирается и подписывается
-- отдельно от базы, и номер, выданный последовательностью, разошёлся бы с
-- ним при первом же переносе.
--
-- # Связь с источником — ключами, а не строкой в теле
--
-- Это главное отличие от MVExams. Там метка лежала строкой внутри body, и
-- два приказа с одинаковой нумерацией пунктов были неразличимы. Здесь
-- источник назван ссылкой, а единица — меткой в его пределах.
--
-- Метка, а не ссылка на строку единицы: задача может быть написана по
-- единице, которой в перечне ещё нет (разбор документа идёт своим чередом),
-- и жёсткий ключ ронял бы генерацию на исправных данных.
CREATE TABLE IF NOT EXISTS cases (
    id           TEXT        PRIMARY KEY,
    source_id    BIGINT      NOT NULL REFERENCES sources (id),
    unit_label   TEXT        NOT NULL DEFAULT '',

    -- Путь единицы на момент публикации. Скопирован сюда намеренно: подбор
    -- задач по срезу пути обязан работать одним запросом, без соединения с
    -- деревом источника, а переписанный путь у старой задачи означал бы
    -- молчаливую смену её места в подборе.
    unit_path    TEXT        NOT NULL DEFAULT '',

    status       TEXT        NOT NULL DEFAULT 'draft'
                             CHECK (status IN ('draft', 'review', 'published', 'archived')),
    -- Оптимистичная блокировка: два редактора, открывшие задачу
    -- одновременно, не затирают друг друга молча.
    revision     INTEGER     NOT NULL DEFAULT 1,
    origin       TEXT        NOT NULL DEFAULT 'manual',

    -- Из какого черновика заведена. Пусто у задач, пришедших ввозом и
    -- написанных руками. Нужно не для истории, а чтобы принять черновик
    -- было можно только однажды: составитель нажимает «Принять» дважды
    -- при обрыве связи, и второе нажатие завело бы вторую задачу с тем же
    -- условием — а заметил бы это не он, а обучающийся, получивший одну
    -- задачу дважды.
    draft_id     BIGINT      NULL REFERENCES case_drafts (id),

    body         JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ NULL
);
-- Указатель частичный: задач без черновика много, и общий указатель
-- считал бы их все одинаковыми.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cases_draft
    ON cases (draft_id) WHERE draft_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cases_status  ON cases (status);
CREATE INDEX IF NOT EXISTS idx_cases_updated ON cases (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_cases_unit    ON cases (source_id, unit_label);
CREATE INDEX IF NOT EXISTS idx_cases_path
    ON cases (source_id, unit_path text_pattern_ops);

-- История правок: нужна не для разбирательства, а для отката — неудачную
-- редактуру врачебного текста надо уметь вернуть, а не переписывать по
-- памяти.
CREATE TABLE IF NOT EXISTS case_revisions (
    case_id  TEXT        NOT NULL REFERENCES cases (id),
    revision INTEGER     NOT NULL,
    status   TEXT        NOT NULL,
    body     JSONB       NOT NULL,
    saved_by TEXT        NOT NULL DEFAULT '',
    saved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (case_id, revision)
);

-- Версия опубликованного содержания. Приложение сравнивает её со своей и
-- качает только при расхождении.
CREATE TABLE IF NOT EXISTS content_version (
    id      BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    version BIGINT  NOT NULL DEFAULT 0
);
INSERT INTO content_version (id, version) VALUES (TRUE, 0)
    ON CONFLICT (id) DO NOTHING;

-- Набор задач — единица доставки и единица продажи одновременно.
--
-- Доставка пакетом, а не лентой, выбрана осознанно: клиент без сети должен
-- видеть задачи, а не пустой экран. Манифест пакета подписан, и приложение
-- сверяет подпись до установки — иначе подменить содержание по дороге
-- может всякий, кто стоит между устройством и сервером.
CREATE TABLE IF NOT EXISTS packs (
    id          BIGSERIAL   PRIMARY KEY,
    slug        TEXT        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]*$'),
    title       TEXT        NOT NULL CHECK (title <> ''),
    summary_md  TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'draft'
                            CHECK (status IN ('draft', 'published', 'retired')),
    -- Линейка: чем набор открывается. Решение владельца 2026-09-20, и оно
    -- же граница бесплатного: гостевой открыт всякому, базовый — тому, кто
    -- привязал почту, платный — подпиской или покупкой, спонсорский открыт
    -- всем и бесплатно, потому что за него уже заплатил спонсор.
    --
    -- Умолчание 'paid' выбрано по видимости ошибки, а не по щедрости.
    -- Набор, закрытый по ошибке, виден сразу — врач его не получит и
    -- скажет; набор, открытый по ошибке, не виден никому, и узнают о нём
    -- по непришедшим деньгам. На живой базе догоняющий накат поставит эту
    -- линейку всем прежним наборам: линейки им назначают в студии, и до
    -- того корпус открыт только купившим.
    line        TEXT        NOT NULL DEFAULT 'paid'
                            CHECK (line IN ('guest', 'basic', 'paid', 'sponsored')),
    -- Редакция набора — и карточки, и состава разом. Одна на двоих
    -- намеренно: правятся они на одном экране, и составитель, чью правку
    -- описания приняли поверх чужой перестановки задач, увидел бы
    -- согласованную неверную картину — набор, которого никто не собирал.
    revision    INTEGER     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pack_items (
    pack_id BIGINT  NOT NULL REFERENCES packs (id),
    case_id TEXT    NOT NULL REFERENCES cases (id),
    ord     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pack_id, case_id)
);

-- Выпуск пакета: то, что уехало на устройства, и его подпись.
--
-- Выпуск неизменяем. Правка пакета — это новый выпуск, а не правка старого:
-- на устройствах стоит именно тот состав, который подписан, и подменить
-- его задним числом значит сделать подпись бессмысленной.
CREATE TABLE IF NOT EXISTS pack_releases (
    id          BIGSERIAL   PRIMARY KEY,
    pack_id     BIGINT      NOT NULL REFERENCES packs (id),
    version     BIGINT      NOT NULL,
    manifest    JSONB       NOT NULL,
    -- Подпись манифеста ed25519. Канонизация манифеста задана эталоном,
    -- общим с приложением: разойдись два способа привести манифест к байтам
    -- — и подпись не сойдётся ни на одном устройстве.
    signature   TEXT        NOT NULL CHECK (signature <> ''),
    key_id      TEXT        NOT NULL CHECK (key_id <> ''),
    released_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pack_id, version)
);

-- Открытые ключи подписи пакетов.
--
-- Закрытая половина живёт в окружении и в базу не попадает никогда.
-- Хранятся здесь ради смены ключа: старые выпуски остаются проверяемыми
-- прежним ключом, новые подписываются новым.
CREATE TABLE IF NOT EXISTS pack_keys (
    key_id     TEXT        PRIMARY KEY,
    public_key TEXT        NOT NULL CHECK (public_key <> ''),
    status     TEXT        NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===========================================================================
-- 4. КЛИЕНТЫ, ДОСТУП И ПРОДАЖИ
-- ===========================================================================

-- Учётная запись клиента.
--
-- Заводится молча при первом запуске приложения: спрашивать имя и почту у
-- человека, который ещё не понял, что ему предлагают, — верный способ его
-- потерять. Почта привязывается позже и делает запись восстановимой: без
-- неё сменивший телефон теряет всё.
CREATE TABLE IF NOT EXISTS accounts (
    id          BIGSERIAL   PRIMARY KEY,
    email       TEXT        NULL UNIQUE,
    display_name TEXT       NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen   TIMESTAMPTZ NULL,
    -- Запись не удаляется, а помечается: на неё ссылаются попытки, права и
    -- платежи, и удаление рвало бы разбирательство о деньгах.
    blocked_at  TIMESTAMPTZ NULL
);

-- Устройства учётной записи.
--
-- Токен непрозрачен и хранится отпечатком, а не текстом: утёкшая база не
-- должна давать входа.
CREATE TABLE IF NOT EXISTS devices (
    id          BIGSERIAL   PRIMARY KEY,
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    token_hash  TEXT        NOT NULL UNIQUE CHECK (token_hash <> ''),
    platform    TEXT        NOT NULL DEFAULT '',
    os_version  TEXT        NOT NULL DEFAULT '',
    model       TEXT        NOT NULL DEFAULT '',
    app_version TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen   TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_devices_account ON devices (account_id);

-- Способы входа. Заведено многострочным сразу: добавить строку дешевле,
-- чем переделывать таблицу с одной колонкой на запись, когда появится
-- второй способ.
CREATE TABLE IF NOT EXISTS account_identities (
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    provider    TEXT        NOT NULL CHECK (provider IN ('email')),
    external_id TEXT        NOT NULL CHECK (external_id <> ''),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, external_id)
);

-- Одноразовые коды на почту: привязка и вход.
CREATE TABLE IF NOT EXISTS email_codes (
    id         BIGSERIAL   PRIMARY KEY,
    account_id BIGINT      NOT NULL REFERENCES accounts (id),
    email      TEXT        NOT NULL CHECK (email <> ''),
    -- Код хранится отпечатком по той же причине, что и токен устройства.
    code_hash  TEXT        NOT NULL CHECK (code_hash <> ''),
    -- Зачем выдан код. Разделено не для отчёта: код привязки уходит на
    -- адрес, который ещё никому не принадлежит, а код возврата доступа —
    -- на уже привязанный, и это две разные двери. Один общий код означал
    -- бы, что выданное для одной двери открывает другую.
    purpose    TEXT        NOT NULL DEFAULT 'bind' CHECK (purpose IN ('bind', 'recovery')),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ NULL,
    attempts   INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_codes_account ON email_codes (account_id, id DESC);

-- Платёж.
--
-- В первом круге источник один — ручной приход оператора. Остальные
-- объявлены сразу, потому что абстракция источника нужна с первого дня:
-- склеивать два учёта задним числом — это миграция по живым деньгам.
CREATE TABLE IF NOT EXISTS payments (
    id             BIGSERIAL   PRIMARY KEY,
    account_id     BIGINT      NOT NULL REFERENCES accounts (id),
    source         TEXT        NOT NULL
                               CHECK (source IN ('operator', 'acquirer', 'store')),
    -- Назначение: 'pack:<slug>' либо 'subscription:<период>'. Строкой, а не
    -- парой колонок, потому что читается человеком в разбирательстве;
    -- закрыто образцом, потому что назначение с опечаткой не найдётся ни
    -- одним отчётом и будет выглядеть отсутствующим.
    purpose        TEXT        NOT NULL
                               CHECK (purpose ~ '^pack:[a-z0-9][a-z0-9-]*$'
                                   OR purpose ~ '^subscription:(month|year)$'),
    amount_kopecks BIGINT      NOT NULL CHECK (amount_kopecks > 0),
    status         TEXT        NOT NULL CHECK (status IN ('created', 'paid', 'refunded')),
    idem_key       TEXT        NOT NULL CHECK (idem_key <> ''),
    -- Опознаватель на стороне эквайера. Пусто у оператора: у прихода,
    -- подтверждённого вне системы, внешнего номера нет, и выдумывать его
    -- нечем.
    external_id    TEXT        NOT NULL DEFAULT '',
    note           TEXT        NOT NULL DEFAULT '',
    refund_note    TEXT        NOT NULL DEFAULT '',
    refunded_at    TIMESTAMPTZ NULL,
    created_by     TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Повтор с тем же ключом не заводит второго платежа, и держит это указатель
-- базы, а не проверка перед вставкой: проверка и вставка — два шага, а
-- соперники видят одно состояние и сталкиваются между ними.
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_idem ON payments (idem_key);
CREATE INDEX IF NOT EXISTS idx_payments_account ON payments (account_id, id DESC);

-- Право на доступ — единственная истина о том, что клиенту открыто.
--
-- Отвязано от способа оплаты намеренно: право выдаётся платежом, живёт само
-- и проверяется одинаково, чем бы ни было куплено — ручным приходом,
-- эквайером или магазином приложений. Два учёта, склеенные задним числом, —
-- это миграция по живым деньгам.
CREATE TABLE IF NOT EXISTS entitlements (
    id          BIGSERIAL   PRIMARY KEY,
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    kind        TEXT        NOT NULL CHECK (kind IN ('pack', 'subscription')),
    pack_id     BIGINT      NULL REFERENCES packs (id),
    origin      TEXT        NOT NULL
                            CHECK (origin IN ('registered', 'purchase', 'subscription', 'grant')),
    starts_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Пусто — бессрочно. Право на набор обычно бессрочно, подписка — нет.
    expires_at  TIMESTAMPTZ NULL,
    revoked_at  TIMESTAMPTZ NULL,
    revoke_note TEXT        NOT NULL DEFAULT '',
    -- Ссылка нужна ради возврата: он ищет право ПО платежу, и без ключа
    -- номер в колонке мог бы указывать на платёж, которого нет, — то есть
    -- возврат молча не нашёл бы что отзывать.
    payment_id  BIGINT      NULL REFERENCES payments (id),
    granted_by  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Род и предмет связаны: право на набор без набора неисполнимо, а
    -- подписка на один набор — не подписка, а право на набор под чужим
    -- именем. Проверяет это база, потому что забыть здесь легче всего.
    CHECK ((kind = 'pack' AND pack_id IS NOT NULL)
        OR (kind = 'subscription' AND pack_id IS NULL))
);
CREATE INDEX IF NOT EXISTS idx_entitlements_account ON entitlements (account_id);

-- Второго действующего права того же происхождения на тот же набор не
-- бывает. Указатель частичный — только по неотозванным: право, отозванное
-- и выданное заново, обязано лечь новой строкой рядом со старой, а полный
-- указатель сделал бы отзыв необратимым.
CREATE UNIQUE INDEX IF NOT EXISTS idx_entitlements_live_pack
    ON entitlements (account_id, pack_id, origin) WHERE revoked_at IS NULL;

-- Цены. В рублях на витрине, в копейках в базе: деньги не хранятся дробным
-- числом никогда.
CREATE TABLE IF NOT EXISTS prices (
    id             BIGSERIAL   PRIMARY KEY,
    -- То же назначение, что у платежа, и тем же образцом: витрина и платёж
    -- обязаны звать один товар одним словом.
    purpose        TEXT        NOT NULL UNIQUE
                               CHECK (purpose ~ '^pack:[a-z0-9][a-z0-9-]*$'
                                   OR purpose ~ '^subscription:(month|year)$'),
    amount_kopecks BIGINT      NOT NULL CHECK (amount_kopecks > 0),
    enabled        BOOLEAN     NOT NULL DEFAULT TRUE,
    -- Редакция цены. Нуль означает «строки не было»: заведение цены и её
    -- правка приходят одной ручкой, и различить их можно только так.
    revision       INTEGER     NOT NULL DEFAULT 1,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Настройки продажи: что показывает витрина и что открыто без денег.
CREATE TABLE IF NOT EXISTS sale_settings (
    id                BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    -- Приём платежей эквайером. Выключен, и включается только после того,
    -- как заведён секрет и проверяется подпись уведомления: настройка,
    -- включённая раньше подписи, открывает всякому желающему ручку,
    -- оформляющую права.
    acquirer_enabled  BOOLEAN     NOT NULL DEFAULT FALSE,
    free_cases_limit  INTEGER     NOT NULL DEFAULT 0,
    offer_url         TEXT        NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO sale_settings (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING;

-- Группа врачей — она же гибкая роль пользователя приложения.
--
-- # Почему группа описывается правилом, а не ярлыком
--
-- Ярлык, навешенный руками, работает на десяти врачах и перестаёт на
-- тысяче: раздать его некому, а через месяц никто не вспомнит, кому и за
-- что он достался. Правило же пересчитывается само, и переставший платить
-- выходит из группы плательщиков в тот же час без чьего-либо участия.
--
-- # Почему правило лежит одним JSONB, а не таблицей условий
--
-- Условия правила не живут по отдельности: их не ищут, на них не
-- ссылаются, и правится правило всегда целиком. Таблица условий дала бы
-- строку, осиротевшую при правке, и порядок, который нужно хранить
-- отдельно. Разбирает и проверяет правило сервер (`internal/audience`), и
-- он же — единственное место, где оно применяется.
CREATE TABLE IF NOT EXISTS audiences (
    id         BIGSERIAL   PRIMARY KEY,
    slug       TEXT        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]*$'),
    title      TEXT        NOT NULL CHECK (title <> ''),
    note       TEXT        NOT NULL DEFAULT '',
    -- Признаки, соединённые «и». Пустое правило НЕ означает «все»: оно
    -- означает, что группа держится поимённым списком, и это ровно то,
    -- что нужно кафедре. «Или» делается тем, что набор открывается сразу
    -- нескольким группам, и тогда оно видно на карточке набора.
    rule       JSONB       NOT NULL DEFAULT '[]'
                           CHECK (jsonb_typeof(rule) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Названные в группе поимённо.
--
-- Поимённый список не заменяет правило, а дополняет его: врач в группе,
-- если подходит по правилу ИЛИ назван здесь. Кафедре, которой выдали
-- доступ списком, правила не подобрать — общего признака у её ординаторов
-- нет, и выдумывать его пришлось бы ради механизма, а не ради дела.
CREATE TABLE IF NOT EXISTS audience_members (
    audience_id BIGINT      NOT NULL REFERENCES audiences (id),
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    -- Кто добавил. Спросят об этом ровно тогда, когда врач скажет, что
    -- доступ у него откуда-то взялся или куда-то делся.
    added_by    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (audience_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_audience_members_account
    ON audience_members (account_id);

-- Кому набор открыт и от кого скрыт помимо линейки.
--
-- Это расширение линейки, а не второй механизм рядом с ней: решает
-- по-прежнему одна функция `packs.OpenTo`, которую зовут витрина, корпус
-- и выгрузка. Встань группы вторым расчётом — витрина показывала бы
-- «открыто» там, где выгрузка отвечает отказом.
--
-- Скрытие сильнее открытия, а купленное сильнее обоих. Порядок этот
-- записан в `packs.OpenTo` и здесь не повторяется: два места для одного
-- правила расходятся молча.
CREATE TABLE IF NOT EXISTS pack_audiences (
    pack_id     BIGINT      NOT NULL REFERENCES packs (id),
    audience_id BIGINT      NOT NULL REFERENCES audiences (id),
    mode        TEXT        NOT NULL CHECK (mode IN ('open', 'hidden')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pack_id, audience_id)
);
CREATE INDEX IF NOT EXISTS idx_pack_audiences_audience
    ON pack_audiences (audience_id);

-- ===========================================================================
-- 5. ПРОГРЕСС, ПОВТОРЕНИЕ И ЗНАКИ
-- ===========================================================================

-- Попытка решения. Первичное событие, из которого считается всё остальное:
-- решаемость задачи, прогресс клиента, сложность, отчёты.
CREATE TABLE IF NOT EXISTS attempts (
    id          BIGSERIAL   PRIMARY KEY,
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    case_id     TEXT        NOT NULL,
    correct     BOOLEAN     NOT NULL,
    -- Что именно выбрали. Нужно для путаницы вариантов: «на что чаще всего
    -- ловятся» — это и есть материал для правки неверных вариантов.
    answer      TEXT        NOT NULL DEFAULT '',
    mode        TEXT        NOT NULL DEFAULT '',
    spent_ms    BIGINT      NOT NULL DEFAULT 0,
    -- Ключ повторности: приложение работает офлайн и досылает попытки
    -- пачкой, повторяя посылку при обрыве. Без ключа одна попытка легла бы
    -- дважды и испортила бы и прогресс, и решаемость.
    idem_key    TEXT        NOT NULL DEFAULT '',
    happened_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_attempts_idem
    ON attempts (account_id, idem_key) WHERE idem_key <> '';
CREATE INDEX IF NOT EXISTS idx_attempts_account ON attempts (account_id, happened_at DESC);
CREATE INDEX IF NOT EXISTS idx_attempts_case    ON attempts (case_id);

-- Состояние повторения по интервалам (SM-2).
--
-- Расчёт живёт на устройстве, а сервер хранит и сводит: иначе клиент без
-- сети не знал бы, что ему повторять. Эталон расчёта общий для сервера и
-- приложения — две реализации сходятся только благодаря ему.
CREATE TABLE IF NOT EXISTS review_states (
    account_id   BIGINT      NOT NULL REFERENCES accounts (id),
    case_id      TEXT        NOT NULL,
    ease         REAL        NOT NULL DEFAULT 2.5,
    interval_days INTEGER    NOT NULL DEFAULT 0,
    repetitions  INTEGER     NOT NULL DEFAULT 0,
    due_at       TIMESTAMPTZ NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, case_id)
);
CREATE INDEX IF NOT EXISTS idx_review_states_due ON review_states (account_id, due_at);

-- Прогресс: опыт, уровень и величины, по которым выдаются знаки.
CREATE TABLE IF NOT EXISTS account_progress (
    account_id BIGINT      PRIMARY KEY REFERENCES accounts (id),
    xp         BIGINT      NOT NULL DEFAULT 0 CHECK (xp >= 0),
    level      INTEGER     NOT NULL DEFAULT 1 CHECK (level >= 1),
    -- Величины каталога: закрытый словарь, зеркальный с приложением.
    -- Считать их двумя разными способами нельзя — расходятся молча.
    metrics    JSONB       NOT NULL DEFAULT '{}' CHECK (metrics <> 'null'::jsonb),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Выпуск знака: сколько их всего и сколько роздано.
--
-- Тираж — свойство знака, а не выдачи: знак с тиражом можно заслужить и не
-- получить, и опыт начисляется по выданному, а не по заслуженному.
CREATE TABLE IF NOT EXISTS sign_editions (
    slug        TEXT        PRIMARY KEY,
    title       TEXT        NOT NULL CHECK (title <> ''),
    -- Пусто — тираж не ограничен.
    edition_size INTEGER    NULL CHECK (edition_size IS NULL OR edition_size > 0),
    issued_count INTEGER    NOT NULL DEFAULT 0 CHECK (issued_count >= 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Выданный знак.
--
-- # Строка не удаляется никогда
--
-- Единственное исключение — переходящий знак: он получает отметку об
-- отзыве, а не удаление, потому что собирательные знаки смотрят на
-- когда-либо выданное. Считай они владение сейчас, кавалерский знак
-- срывался бы вслед за переходящим, и собрать орден было бы нельзя в
-- принципе.
--
-- Номер выдаёт сервер, и выдаёт указателем базы, а не счётчиком в коде:
-- два соперника, посчитавшие «следующий номер» по одному и тому же
-- состоянию, выдали бы один номер двоим.
CREATE TABLE IF NOT EXISTS account_signs (
    id          BIGSERIAL   PRIMARY KEY,
    account_id  BIGINT      NOT NULL REFERENCES accounts (id),
    sign_slug   TEXT        NOT NULL REFERENCES sign_editions (slug),
    serial      INTEGER     NOT NULL CHECK (serial > 0),
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ NULL,
    UNIQUE (sign_slug, serial)
);
CREATE INDEX IF NOT EXISTS idx_account_signs_account ON account_signs (account_id);

-- Свидетельство о знаке: по нему знак проверяется снаружи, без входа.
CREATE TABLE IF NOT EXISTS sign_certificates (
    token      TEXT        PRIMARY KEY,
    sign_id    BIGINT      NOT NULL REFERENCES account_signs (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Настройки игровой механики: пороги уровней, цена действий в опыте.
--
-- В базе, а не в сборке: их правят, глядя на поведение клиентов, и сборка
-- ради порога не выпускается. Приложение читает их с сервера и считает по
-- ним само — по тому же эталону, что и сервер.
CREATE TABLE IF NOT EXISTS game_configs (
    id         BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    body       JSONB       NOT NULL DEFAULT '{}' CHECK (body <> 'null'::jsonb),
    revision   INTEGER     NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO game_configs (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING;

-- Покупка за опыт: место, где игровое сходится с продажами.
CREATE TABLE IF NOT EXISTS xp_spends (
    id         BIGSERIAL   PRIMARY KEY,
    account_id BIGINT      NOT NULL REFERENCES accounts (id),
    purpose    TEXT        NOT NULL CHECK (purpose <> ''),
    xp_amount  BIGINT      NOT NULL CHECK (xp_amount > 0),
    idem_key   TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_xp_spends_idem
    ON xp_spends (account_id, idem_key) WHERE idem_key <> '';

-- ===========================================================================
-- 6. АНАЛИЗ ПОВЕДЕНИЯ
-- ===========================================================================

-- События поведения.
--
-- Словарь событий закрыт и зеркален с приложением: событие, которого нет в
-- словаре, отбрасывается, а не записывается «на всякий случай». Открытый
-- словарь превращает телеметрию в свалку, по которой нельзя построить ни
-- одного отчёта.
CREATE TABLE IF NOT EXISTS telemetry_events (
    id          BIGSERIAL   PRIMARY KEY,
    account_id  BIGINT      NULL REFERENCES accounts (id),
    session_id  TEXT        NOT NULL DEFAULT '',
    name        TEXT        NOT NULL CHECK (name <> ''),
    props       JSONB       NOT NULL DEFAULT '{}' CHECK (props <> 'null'::jsonb),
    app_version TEXT        NOT NULL DEFAULT '',
    idem_key    TEXT        NOT NULL DEFAULT '',
    happened_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_telemetry_idem
    ON telemetry_events (account_id, idem_key) WHERE idem_key <> '';
CREATE INDEX IF NOT EXISTS idx_telemetry_time ON telemetry_events (happened_at DESC);
CREATE INDEX IF NOT EXISTS idx_telemetry_name ON telemetry_events (name, happened_at DESC);

-- Опыты и распределение клиентов по ветвям.
--
-- Ветвь назначается один раз и хранится: клиент, которому ветвь считают
-- заново при каждом запуске, кочует между ними, и опыт меряет шум.
CREATE TABLE IF NOT EXISTS experiments (
    slug       TEXT        PRIMARY KEY,
    title      TEXT        NOT NULL DEFAULT '',
    status     TEXT        NOT NULL DEFAULT 'draft'
                           CHECK (status IN ('draft', 'running', 'stopped')),
    arms       JSONB       NOT NULL DEFAULT '[]' CHECK (arms <> 'null'::jsonb),
    started_at TIMESTAMPTZ NULL,
    stopped_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS experiment_arms (
    experiment_slug TEXT        NOT NULL REFERENCES experiments (slug),
    account_id      BIGINT      NOT NULL REFERENCES accounts (id),
    arm             TEXT        NOT NULL CHECK (arm <> ''),
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (experiment_slug, account_id)
);

-- Сводка по задаче: решаемость и путаница вариантов.
--
-- Считается из попыток, но хранится отдельно: отчёт, который каждый раз
-- перебирает все попытки, перестаёт открываться ровно тогда, когда данных
-- становится достаточно, чтобы он был интересен.
--
-- Эта же таблица — место, куда ляжет ввоз решаемости из старой базы:
-- сводкой по задаче, а не попытками. Попытки принадлежат клиентам, которых
-- в новой базе нет, а нужна из них ровно доля решивших.
CREATE TABLE IF NOT EXISTS case_stats (
    case_id       TEXT        PRIMARY KEY,
    attempts      BIGINT      NOT NULL DEFAULT 0,
    correct       BIGINT      NOT NULL DEFAULT 0,
    -- Доля решивших, посчитанная заранее: по ней идёт отбор «слишком
    -- лёгких» и «неразрешимых», и считать её в запросе на каждый показ
    -- незачем.
    solve_rate    REAL        NOT NULL DEFAULT 0,
    median_ms     BIGINT      NOT NULL DEFAULT 0,
    -- На что ловятся: вариант и сколько раз его выбрали.
    confusion     JSONB       NOT NULL DEFAULT '{}' CHECK (confusion <> 'null'::jsonb),
    -- Откуда пришли числа: 'live' — из наших попыток, 'imported' — ввезено
    -- из прежней базы. Различать обязательно: ввезённая решаемость мерила
    -- другую аудиторию, и смешивать её с нашей молча нельзя.
    origin        TEXT        NOT NULL DEFAULT 'live'
                              CHECK (origin IN ('live', 'imported')),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Указатель сведения: докуда сводка уже посчитана.
--
-- Без него пересчёт начинался бы с начала времён и с каждым днём шёл бы
-- дольше.
CREATE TABLE IF NOT EXISTS rollup_cursors (
    name       TEXT        PRIMARY KEY,
    position   BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Дневная сводка по клиенту: чем он занимался и сколько.
CREATE TABLE IF NOT EXISTS rollup_account_days (
    account_id BIGINT  NOT NULL REFERENCES accounts (id),
    day        DATE    NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    correct    INTEGER NOT NULL DEFAULT 0,
    spent_ms   BIGINT  NOT NULL DEFAULT 0,
    xp_gained  BIGINT  NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, day)
);

-- ===========================================================================
-- 7. СЛУЖЕБНОЕ
-- ===========================================================================

-- Пользователи студии: именованный вход, своя роль, свои права.
--
-- Право проверяется на сервере, а не скрытием раздела в интерфейсе: скрытая
-- вкладка при открытой ручке — подсказка, где искать, а не запрет.
CREATE TABLE IF NOT EXISTS users (
    id           BIGSERIAL   PRIMARY KEY,
    login        TEXT        NOT NULL UNIQUE CHECK (login <> ''),
    display_name TEXT        NOT NULL DEFAULT '',
    -- Секрет одноразовых кодов. Хранится здесь, а не в окружении: людей
    -- несколько, и у каждого свой.
    totp_secret  TEXT        NOT NULL DEFAULT '',
    -- Права списком, а не ролью с зашитым набором: роль «редактор» у двух
    -- проектов означает разное, а список говорит сам за себя.
    permissions  TEXT[]      NOT NULL DEFAULT '{}',
    disabled_at  TIMESTAMPTZ NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Сессии студии. Токен хранится отпечатком по той же причине, что токен
-- устройства.
CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash TEXT        PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Журнал действий студии: кто что сделал.
--
-- Пишется решение, а не нажатие: в разбирательстве важно, что именно
-- изменилось и по чьему слову.
CREATE TABLE IF NOT EXISTS admin_journal (
    id         BIGSERIAL   PRIMARY KEY,
    user_login TEXT        NOT NULL DEFAULT '',
    action     TEXT        NOT NULL CHECK (action <> ''),
    subject    TEXT        NOT NULL DEFAULT '',
    details    JSONB       NOT NULL DEFAULT '{}' CHECK (details <> 'null'::jsonb),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_journal_time ON admin_journal (created_at DESC);

-- Ключи программ: чем приложение представляется серверу.
CREATE TABLE IF NOT EXISTS app_keys (
    key_id     TEXT        PRIMARY KEY,
    secret_hash TEXT       NOT NULL CHECK (secret_hash <> ''),
    title      TEXT        NOT NULL DEFAULT '',
    disabled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ключи повторности для платных и необратимых операций.
--
-- Ответ хранится вместе с ключом: повтор обязан получить ТОТ ЖЕ ответ, а не
-- отказ «уже сделано» — иначе клиент, потерявший ответ из-за обрыва, не
-- узнает, чем кончилась его операция.
--
-- НО СЕГОДНЯ ЭТА ТАБЛИЦА ПУСТА, и говорится это здесь, а не в отдельном
-- документе: читающий схему в поисках повторности находит первым делом её и
-- уходит уверенный, что механика есть. Механика есть, и она в другом месте —
-- единственное необратимое действие, приём прихода, держит повторность
-- указателем payments.idem_key, и повтор получает там ТОТ ЖЕ платёж
-- (internal/sales/payments.go, Accept). Отдельное хранилище ключей ему не
-- нужно: ответ и есть строка платежа.
--
-- Таблица не убрана, потому что схема растёт только добавлением, а
-- заполнять её наполовину нельзя тем более: ключ, записанный сюда без
-- ответа, превращает повтор в отказ «уже сделано» — ровно в то, от чего
-- пояснение выше и бережёт. Появится второе необратимое действие, которому
-- своего idem_key мало, — механика пишется тогда и целиком.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key        TEXT        PRIMARY KEY,
    scope      TEXT        NOT NULL DEFAULT '',
    response   JSONB       NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Настройки службы одной строкой: то, что правится в студии и не заслуживает
-- своей таблицы.
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT        PRIMARY KEY,
    value      JSONB       NOT NULL CHECK (value <> 'null'::jsonb),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
