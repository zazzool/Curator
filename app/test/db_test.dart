/// Проверки местной базы.
///
/// Миграции — самый рискованный класс изменений в приложении: ошибка здесь
/// стирает прогресс на установленных устройствах, и починить её нечем.
/// Поэтому подъём проверяется СО ВСЕХ прежних схем, а не только с чистой.
library;

import 'dart:io';

import 'package:curator/cases/outbox.dart';
import 'package:curator/db/database.dart';
import 'package:curator/db/outbox_store.dart';
import 'package:curator/db/schedule.dart';
import 'package:curator/progress/rules.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// Объявления таблиц и указателей — то, чем схемы сравниваются.
Future<List<String>> shape(Database db) async {
  final rows = await db.query(
    'sqlite_master',
    columns: ['type', 'name', 'sql'],
    where: "name NOT LIKE 'sqlite_%'",
    orderBy: 'name',
  );
  return [
    for (final row in rows) '${row['type']} ${row['name']}: ${row['sql']}',
  ];
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
  });

  final factory = databaseFactoryFfi;

  test('база поднимается с чистого места', () async {
    final db = await openLocalDatabase(
      factory: factory,
      path: inMemoryDatabasePath,
    );
    addTearDown(db.close);
    expect(await db.getVersion(), schemaVersion);
    expect(await shape(db), isNotEmpty);
  });

  test('испорченный файл отводится в сторону, база открывается', () async {
    // Испорченный файл не станет целым ни от перезапуска, ни от ожидания.
    // Прежде он оставался лежать, `openLocalDatabase` отказывала каждый
    // раз, и повторение без сети со справочником были выключены навсегда
    // — молча: приложение поднималось как ни в чём не бывало.
    final dir = await Directory.systemTemp.createTemp('curator-db');
    addTearDown(() => dir.delete(recursive: true));
    final path = '${dir.path}/curator.db';
    await File(path).writeAsString('это не база, а мусор');

    final db = await openLocalDatabaseRecovering(factory: factory, path: path);
    addTearDown(db.close);

    expect(await db.getVersion(), schemaVersion);
    expect(await shape(db), isNotEmpty);
    // В сторону, а не насовсем: «испорчена» — наше суждение по словам
    // SQLite, и ошибись оно, удалёнными оказались бы месяцы занятий.
    expect(File('$path.broken').existsSync(), isTrue);
  });

  test('база новее приложения не трогается', () async {
    // Второй случай того же отказа, и лечится он противоположно: файл
    // положен более новой сборкой, поэтапная выкладка вернёт её врачу
    // через день-другой, и отвести такой файл в сторону значит стереть
    // его занятия ради сообщения в журнале.
    final dir = await Directory.systemTemp.createTemp('curator-db');
    addTearDown(() => dir.delete(recursive: true));
    final path = '${dir.path}/curator.db';

    final ahead = await factory.openDatabase(
      path,
      options: OpenDatabaseOptions(
        version: schemaVersion + 1,
        onCreate: (db, version) async {},
      ),
    );
    await ahead.close();

    Object? thrown;
    try {
      await openLocalDatabaseRecovering(factory: factory, path: path);
    } catch (error) {
      thrown = error;
    }

    expect(thrown, isA<LocalDatabaseTooNew>(), reason: 'понижение прошло');
    expect(looksCorrupt(thrown!), isFalse);
    expect(File('$path.broken').existsSync(), isFalse);
    expect(File(path).existsSync(), isTrue);
  });

  test('подъём со всякой прежней схемы даёт ту же базу', () async {
    // Ради этого всё и написано. Устройство, пропустившее две версии,
    // обязано прийти ровно к той же схеме, что и свежая установка.
    final fresh = await openLocalDatabase(
      factory: factory,
      path: inMemoryDatabasePath,
    );
    addTearDown(fresh.close);
    final want = await shape(fresh);

    for (var from = 1; from < schemaVersion; from++) {
      // Поднимаем базу такой, какой её видела сборка версии `from`…
      final old = await factory.openDatabase(
        inMemoryDatabasePath,
        options: OpenDatabaseOptions(
          version: from,
          singleInstance: false,
          onCreate: (db, _) async {
            for (var step = 0; step < from; step++) {
              await db.execute(migrations[step]);
            }
          },
        ),
      );
      // …и доводим её до нынешней тем же накатом, что стоит в приложении.
      for (var step = from; step < schemaVersion; step++) {
        await old.execute(migrations[step]);
      }
      await old.setVersion(schemaVersion);

      expect(
        await shape(old),
        want,
        reason: 'база, поднятая с версии $from, разошлась со свежей',
      );
      await old.close();
    }
  });

  test('каждый шаг наката идемпотентен', () async {
    // Накат, применённый дважды, обязан пройти молча: иначе прерванное
    // обновление оставляет устройство с базой, которую нельзя открыть.
    final db = await openLocalDatabase(
      factory: factory,
      path: inMemoryDatabasePath,
    );
    addTearDown(db.close);
    for (final step in migrations) {
      await db.execute(step);
    }
    expect(await shape(db), isNotEmpty);
  });

  test('ни один шаг ничего не удаляет и не переименовывает', () async {
    // Переименование колонки на устройстве, пропустившем две версии, —
    // это потерянные данные. Проверка глупая нарочно: её задача —
    // краснеть в тот день, когда кто-то допишет DROP «по-быстрому».
    for (final step in migrations) {
      final words = step.toUpperCase();
      expect(words.contains('DROP '), isFalse, reason: step);
      expect(words.contains('RENAME'), isFalse, reason: step);
      expect(words.contains('DELETE '), isFalse, reason: step);
    }
  });

  test('версия схемы считается по шагам, а не пишется рядом', () {
    // Два места для одного числа расходятся молча, и здесь расхождение
    // означает пропущенный шаг у части устройств.
    expect(schemaVersion, migrations.length);
  });

  group('расписание', () {
    late Database db;
    late Schedule schedule;

    setUp(() async {
      db = await openLocalDatabase(
        factory: factory,
        path: inMemoryDatabasePath,
      );
      schedule = Schedule(db);
    });

    tearDown(() => db.close());

    test('состояние после ответа переживает перезапуск', () async {
      final rules = Rules.defaults;
      final now = DateTime(2026, 9, 19, 10);
      final after = rules.next(rules.fresh, true, now);

      await schedule.put('c-01', after);

      final back = await schedule.state('c-01');
      expect(back!.ease, after.ease);
      expect(back.intervalDays, after.intervalDays);
      expect(back.repetitions, after.repetitions);
      expect(back.dueAt!.toUtc(), after.dueAt!.toUtc());
    });

    test('просроченные отдаются по возрастанию срока', () async {
      // Дольше всех ждавшая задача забыта сильнее: показать её последней
      // значит потерять её же.
      final now = DateTime(2026, 9, 19, 10);
      await schedule.put(
        'c-поздняя',
        ReviewState(ease: 2.5, dueAt: now.subtract(const Duration(hours: 1))),
      );
      await schedule.put(
        'c-ранняя',
        ReviewState(ease: 2.5, dueAt: now.subtract(const Duration(days: 5))),
      );
      await schedule.put(
        'c-будущая',
        ReviewState(ease: 2.5, dueAt: now.add(const Duration(days: 3))),
      );

      final due = await schedule.due(now);
      expect([for (final one in due) one.caseId], ['c-ранняя', 'c-поздняя']);
    });

    test('задача без срока в повторение не идёт', () async {
      // Свежая задача — это лента, а не повторение.
      await schedule.put('c-01', Rules.defaults.fresh);
      expect(await schedule.due(DateTime(2026, 9, 19)), isEmpty);
    });

    test('сервер не затирает разбор, который до него не доехал', () async {
      // Иначе задача, разобранная в метро, вернулась бы к повторению
      // сразу после выхода на связь.
      final now = DateTime(2026, 9, 19, 10);
      final mine = ReviewState(
        ease: 2.6,
        intervalDays: 6,
        repetitions: 2,
        dueAt: now.add(const Duration(days: 6)),
      );
      await schedule.put('c-01', mine);

      final taken = await schedule.accept({'c-01': now});

      expect(taken, 0);
      expect((await schedule.state('c-01'))!.intervalDays, 6);
    });

    test('доехавший разбор сервер перекрывает сроком, не расчётом', () async {
      // Сверяется и то, что срок переписан, И то, что расчёт уцелел.
      //
      // Прежде здесь сверялся один срок, и проверка проходила бы, даже
      // если бы accept не делал ничего, кроме записи срока. Она и
      // проходила: INSERT OR REPLACE обнулял лёгкость, интервал и число
      // повторов, а набор оставался зелёным. Сервер владеет сроком;
      // расчёт посчитан здесь по общему эталону, и затирать его нечем.
      final now = DateTime(2026, 9, 19, 10);
      await schedule.put(
        'c-01',
        ReviewState(
          ease: 2.6,
          intervalDays: 15,
          repetitions: 4,
          dueAt: now.add(const Duration(days: 15)),
        ),
        synced: true,
      );

      final taken = await schedule.accept({'c-01': now});

      expect(taken, 1);
      final after = (await schedule.state('c-01'))!;
      expect(after.dueAt!.toUtc(), now.toUtc());
      expect(after.ease, 2.6);
      expect(after.intervalDays, 15);
      expect(after.repetitions, 4);
    });

    test('незнакомая задача от сервера просто ложится', () async {
      final now = DateTime(2026, 9, 19, 10);
      expect(await schedule.accept({'c-новая': now}), 1);
      expect(await schedule.state('c-новая'), isNotNull);
    });

    test('отметка о доставке снимает запрет перекрывать', () async {
      final now = DateTime(2026, 9, 19, 10);
      await schedule.put('c-01', ReviewState(ease: 2.5, dueAt: now));
      expect(await schedule.accept({'c-01': now}), 0);

      await schedule.markSynced(['c-01']);
      expect(await schedule.accept({'c-01': now}), 1);
    });
  });

  group('очередь разборов', () {
    late Database db;
    late DbOutboxStore store;

    setUp(() async {
      db = await openLocalDatabase(
        factory: factory,
        path: inMemoryDatabasePath,
      );
      store = DbOutboxStore(db);
    });

    tearDown(() => db.close());

    PendingAttempt attempt(String key, {int minute = 0}) => PendingAttempt(
      caseId: 'c-01',
      correct: true,
      answer: 'Верный',
      mode: 'feed',
      spentMs: 4200,
      idemKey: key,
      happenedAt: DateTime.utc(2026, 9, 19, 10, minute),
    );

    test('разбор переживает перезапуск целым', () async {
      await store.write([attempt('k-1').toJson()]);

      final back = PendingAttempt.tryParse((await store.read()).single)!;
      expect(back.idemKey, 'k-1');
      expect(back.answer, 'Верный');
      expect(back.spentMs, 4200);
      expect(back.happenedAt.toUtc(), DateTime.utc(2026, 9, 19, 10));
    });

    test('очередь отдаётся в порядке появления', () async {
      // Разбор, сделанный раньше, и уехать должен раньше: иначе
      // расписание на сервере пересчитается задом наперёд.
      await store.write([
        attempt('k-3', minute: 30).toJson(),
        attempt('k-1', minute: 10).toJson(),
        attempt('k-2', minute: 20).toJson(),
      ]);

      expect(
        [for (final row in await store.read()) row['idemKey']],
        ['k-1', 'k-2', 'k-3'],
      );
    });

    test('один ключ повторности ложится один раз', () async {
      // Дважды положенная попытка портит и прогресс, и решаемость, причём
      // незаметно: числа остаются правдоподобными.
      await store.write([attempt('k-1').toJson(), attempt('k-1').toJson()]);
      expect((await store.read()).length, 1);
    });

    test('негодная строка не ложится, а очередь остаётся', () async {
      await store.write([
        attempt('k-1').toJson(),
        {'idemKey': '', 'caseId': 'c-02'},
      ]);
      expect((await store.read()).length, 1);
    });

    test('прежняя очередь переезжает, а не пропадает', () async {
      // На устройствах, обновившихся с прежней сборки, в настройках лежит
      // чей-то месяц занятий.
      final older = MemoryOutboxStore();
      await older.write([attempt('k-старый').toJson()]);
      await store.write([attempt('k-новый', minute: 40).toJson()]);

      expect(await store.adopt(older), 1);

      expect(
        [for (final row in await store.read()) row['idemKey']],
        ['k-старый', 'k-новый'],
      );
      // Прежнее место очищено — переносить второй раз нечего.
      expect(await older.read(), isEmpty);
      expect(await store.adopt(older), 0);
    });
  });
}
