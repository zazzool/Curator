/// Местная база устройства.
///
/// # Почему миграции — самый рискованный класс изменений
///
/// Ошибка здесь стирает прогресс на установленных устройствах, и починить
/// её нечем: сборка обновится не завтра, а стёртое не вернётся вовсе.
/// Поэтому правила ниже жёсткие и не смягчаются «на этот раз».
///
/// # Схема растёт только добавлением
///
/// Ровно как на сервере. Каждый шаг идемпотентен (`IF NOT EXISTS`), ни
/// один не удаляет и не переименовывает — переименование колонки на
/// устройстве, которое пропустило две версии, это потерянные данные.
///
/// # Шаги нумеруются и не переписываются
///
/// Выпущенный шаг застывает. Правка выпущенного шага меняет схему только у
/// тех, кто ставит приложение впервые, а у остальных база остаётся прежней
/// — и две установки одной версии начинают расходиться молча.
///
/// # Подъём идёт со всех прежних схем, и это проверяется
///
/// Проверка поднимает базу с версии 1, с версии 2 и так далее до текущей,
/// и сверяет, что получилось одно и то же. Без такой проверки миграция
/// проверяется только на телефоне — то есть уже после выкладки.
///
/// # Чего эта проверка НЕ ловит, и что с этим делать
///
/// Правку **выпущенного** шага она не поймает: обе стороны сверки читают
/// один и тот же список, и отредактированный шаг применится в обеих. Чтобы
/// ловилось и это, нужен записанный снимок каждой выпущенной схемы — как
/// `shared/wire-contract.json` стережёт проводные форматы.
///
/// Снимка здесь пока нет намеренно: **ни одна сборка ещё не вышла**, и до
/// первой выкладки схема правится свободно. Это то же окно, что открыто у
/// проводных форматов, и закрывается оно тем же событием. В день первой
/// выкладки сюда кладётся снимок схемы версии 1, и дальше каждая выкладка
/// добавляет свой. До тех пор запрет «выпущенный шаг застыл» держится
/// договорённостью, а не проверкой, и знать об этом нужно именно потому,
/// что выглядит всё одинаково зелёным.
library;

import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:sqflite/sqflite.dart';

/// База на устройстве новее приложения.
///
/// Своим родом, а не `StateError` с текстом: отличать этот случай от
/// испорченного файла приходится в подъёме приложения, и отличать его
/// разбором сообщения — значит сломать различение первой же правкой
/// текста. Разница тут дорогая: испорченный файл незачем беречь, а этот
/// — это данные врача, положенные более новой сборкой, и поэтапная
/// выкладка вернёт ему её через день-другой.
class LocalDatabaseTooNew implements Exception {
  const LocalDatabaseTooNew(this.have, this.want);

  /// Версия схемы, лежащая на устройстве.
  final int have;

  /// Версия схемы, которую знает эта сборка.
  final int want;

  @override
  String toString() =>
      'база устройства новее приложения ($have против $want): '
      'обновите приложение';
}

/// Шаги наката. Порядок — это и есть версия базы.
///
/// Новый шаг дописывается в конец. Ничто здесь не правится и не
/// выбрасывается: выпущенный шаг застыл.
const migrations = <String>[
  // 1. Расписание повторения по каждой задаче.
  //
  // Состояние лежит на устройстве, потому что повторение обязано работать
  // без сети. Хозяином остаётся сервер: он видит все устройства врача, а
  // это — местная копия, которая уступает серверной при расхождении.
  '''
  CREATE TABLE IF NOT EXISTS review (
    case_id     TEXT PRIMARY KEY,
    ease        REAL    NOT NULL DEFAULT 0,
    interval_d  INTEGER NOT NULL DEFAULT 0,
    repetitions INTEGER NOT NULL DEFAULT 0,
    due_at      INTEGER NOT NULL DEFAULT 0,
    synced      INTEGER NOT NULL DEFAULT 0
  )
  ''',

  // 2. Выборка «что просрочено на сегодня» — ради неё база и заведена.
  //
  // Без указателя это перебор всего, что лежит, и на тысяче задач его
  // видно врачу: экран повторения открывается с задержкой.
  'CREATE INDEX IF NOT EXISTS review_due ON review (due_at)',

  // 3. Очередь разборов, ожидающих отправки.
  //
  // Переехала сюда из настроек: настройки на Android читаются в память
  // целиком при запуске приложения, а очередь за месяц без сети вырастает.
  // Ключ повторности выдаёт устройство — он же и первичный ключ, потому
  // что дважды положенная попытка портит и прогресс, и решаемость, причём
  // незаметно: числа остаются правдоподобными.
  '''
  CREATE TABLE IF NOT EXISTS outbox (
    idem_key    TEXT PRIMARY KEY,
    case_id     TEXT    NOT NULL,
    correct     INTEGER NOT NULL DEFAULT 0,
    answer      TEXT    NOT NULL DEFAULT '',
    mode        TEXT    NOT NULL DEFAULT '',
    spent_ms    INTEGER NOT NULL DEFAULT 0,
    happened_at INTEGER NOT NULL DEFAULT 0
  )
  ''',

  // 4. Отправляется в порядке появления: разбор, сделанный раньше, и
  //    уехать должен раньше — иначе расписание на сервере пересчитается
  //    задом наперёд.
  'CREATE INDEX IF NOT EXISTS outbox_order ON outbox (happened_at)',

  // 5. Справочник на устройстве: какие источники скачаны и какого они
  //    выпуска.
  //
  //    Выпуск хранится рядом с копией, а не в настройках: настройки и
  //    база — два места, которые расходятся при любом обрыве посреди
  //    закачки, и разойдясь, они дают худшее из возможного — число
  //    говорит «свежая», а лежат половина рубрик.
  '''
  CREATE TABLE IF NOT EXISTS ref_sources (
    slug           TEXT PRIMARY KEY,
    title          TEXT    NOT NULL DEFAULT '',
    unit_word      TEXT    NOT NULL DEFAULT '',
    statement_word TEXT    NOT NULL DEFAULT '',
    edition        TEXT    NOT NULL DEFAULT '',
    version        INTEGER NOT NULL DEFAULT 0,
    units          INTEGER NOT NULL DEFAULT 0,
    statements     INTEGER NOT NULL DEFAULT 0,
    synced_at      INTEGER NOT NULL DEFAULT 0
  )
  ''',

  // 6. Единицы справочника. Ключ составной: метка уникальна внутри
  //    источника, а не вообще — «п. 1» есть в каждом приказе.
  '''
  CREATE TABLE IF NOT EXISTS ref_units (
    source_slug  TEXT    NOT NULL,
    label        TEXT    NOT NULL,
    parent_label TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',
    path         TEXT    NOT NULL DEFAULT '',
    depth        INTEGER NOT NULL DEFAULT 0,
    kind         TEXT    NOT NULL DEFAULT 'entry',
    answerable   INTEGER NOT NULL DEFAULT 1,
    statements   INTEGER NOT NULL DEFAULT 0,
    ord          INTEGER NOT NULL DEFAULT 0,

    -- Метка и название одной строкой, свёрнутые в нижний регистр. Заведена
    -- ради поиска: LIKE в SQLite нечувствителен к регистру только для
    -- латиницы, и «депресс» не находило «Депрессивный эпизод» — то есть
    -- поиск не работал ровно на том языке, на котором написан справочник.
    -- Свёртка делается в Dart: LOWER() в SQLite спотыкается на кириллице
    -- так же, как LIKE.
    search       TEXT    NOT NULL DEFAULT '',

    PRIMARY KEY (source_slug, label)
  )
  ''',

  // 7. Спуск по дереву — то, чем открывается справочник, и без указателя
  //    это перебор всех рубрик на каждое касание.
  'CREATE INDEX IF NOT EXISTS ref_units_children ON ref_units (source_slug, parent_label, ord)',

  // 8. Положения единиц: критерии, пункты, абзацы.
  '''
  CREATE TABLE IF NOT EXISTS ref_statements (
    id          INTEGER NOT NULL,
    source_slug TEXT    NOT NULL,
    unit_label  TEXT    NOT NULL,
    kind        TEXT    NOT NULL DEFAULT '',
    designation TEXT    NOT NULL DEFAULT '',
    place_ref   TEXT    NOT NULL DEFAULT '',
    body_md     TEXT    NOT NULL DEFAULT '',
    ord         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (source_slug, id)
  )
  ''',

  // 9. Критерии открываемой рубрики — второй по частоте запрос после
  //    спуска по дереву.
  'CREATE INDEX IF NOT EXISTS ref_statements_unit ON ref_statements (source_slug, unit_label, ord)',
];

/// Версия схемы — это просто число шагов.
///
/// Считается, а не пишется рядом: два места для одного числа расходятся
/// молча, и здесь расхождение означает пропущенный шаг у части устройств.
int get schemaVersion => migrations.length;

/// Открывает базу и доводит её до текущей схемы.
///
/// [factory] подменяется в проверках: настоящий sqflite живёт на
/// устройстве, а миграции обязаны проверяться до выкладки.
Future<Database> openLocalDatabase({
  DatabaseFactory? factory,
  String path = 'curator.db',
}) async {
  final open = factory ?? databaseFactory;
  return open.openDatabase(
    path,
    options: OpenDatabaseOptions(
      version: schemaVersion,
      onCreate: (db, version) => _grow(db, 0, version),
      onUpgrade: (db, from, to) => _grow(db, from, to),
      // Понижения не бывает: старая сборка на новой базе — это либо откат
      // выкладки, либо чужой файл. Ни в том, ни в другом случае трогать
      // данные нельзя, и отказ здесь честнее молчаливой порчи.
      onDowngrade: (db, from, to) async {
        throw LocalDatabaseTooNew(from, to);
      },
    ),
  );
}

Future<void> _grow(Database db, int from, int to) async {
  for (var step = from; step < to && step < migrations.length; step++) {
    await db.execute(migrations[step]);
  }
}

/// Открывает базу, а испорченный файл отводит в сторону и пробует снова.
///
/// # Зачем
///
/// База не открывается по двум разным причинам, и лечатся они
/// противоположно. Файл, положенный более новой сборкой, трогать нельзя:
/// это прогресс врача, и поэтапная выкладка вернёт ему новую сборку сама.
/// А испорченный файл не станет целым ни от перезапуска, ни от ожидания —
/// пока он лежит, повторение без сети и справочник не работают вовсе, и
/// врач об этом даже не узнает: приложение поднимается молча.
///
/// # Почему в сторону, а не насовсем
///
/// Переименование, а не удаление: «испорчена» — это наше суждение по
/// словам SQLite, и ошибись мы, удалёнными окажутся месяцы занятий.
/// Имя у отведённого файла одно и то же: копии с отметками времени
/// копились бы на устройстве, и последней бедой врача стало бы место.
///
/// Спутники `-wal` и `-shm` убираются вместе с файлом: без своей базы они
/// бессмысленны, а оставь их рядом — новая база подхватила бы чужой
/// журнал и испортилась бы следом.
Future<Database> openLocalDatabaseRecovering({
  DatabaseFactory? factory,
  String path = 'curator.db',
}) async {
  final open = factory ?? databaseFactory;
  try {
    return await openLocalDatabase(factory: open, path: path);
  } catch (error) {
    if (!looksCorrupt(error)) rethrow;
    debugPrint('база устройства испорчена, отводим в сторону: $error');
    await _setAside(open, path);
    return openLocalDatabase(factory: open, path: path);
  }
}

/// Испорчен ли файл базы — по словам самой SQLite.
///
/// Узнаётся положительно, по названным словам отказа, а не как «всё
/// остальное». Отвести в сторону можно только то, что уже негодно: отказ
/// бывает и «нет места», и «файл занят», и база при этом цела — а
/// отведённая в сторону целая база это стёртый прогресс.
bool looksCorrupt(Object error) {
  final said = error.toString().toLowerCase();
  // «не база», а не «file is not a database» целиком: у разных выпусков
  // SQLite этот отказ звучит ещё и как «file is encrypted or is not a
  // database», и точное совпадение пропустило бы половину случаев.
  return said.contains('not a database') ||
      said.contains('database disk image is malformed') ||
      said.contains('sqlite_corrupt') ||
      said.contains('sqlite_notadb');
}

Future<void> _setAside(DatabaseFactory open, String path) async {
  final full = await _fullPath(open, path);
  final file = File(full);
  if (await file.exists()) await file.rename('$full.broken');
  for (final tail in const ['-wal', '-shm']) {
    final spare = File('$full$tail');
    if (await spare.exists()) await spare.delete();
  }
}

/// Полный путь к файлу базы.
///
/// Разделитель один, и это не упущение: приложение выкладывается под
/// Android, а проверки идут на Linux. Появится вторая платформа — сюда же
/// приедет и её разделитель, а тянуть ради одной строки ещё одну
/// зависимость незачем.
Future<String> _fullPath(DatabaseFactory open, String path) async {
  if (path.startsWith('/')) return path;
  final dir = await open.getDatabasesPath();
  return dir.endsWith('/') ? '$dir$path' : '$dir/$path';
}
