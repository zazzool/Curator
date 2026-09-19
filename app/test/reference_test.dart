/// Проверки справочника: разбор ответа, хранение, закачка.
///
/// Экранов здесь нет намеренно: проверка виджетов поднимает свою привязку
/// на весь набор, подменяет `HttpClient` и отвечает 400 на всякий запрос, а
/// настоящая база внутри неё не открывается вовсе. Показ проверяется
/// отдельно (`reference_view_test.dart`).
library;

import 'package:curator/api/client.dart';
import 'package:curator/db/database.dart';
import 'package:curator/db/reference_store.dart';
import 'package:curator/reference/model.dart';
import 'package:curator/reference/sync.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'fake_server.dart';

RefUnit unit(
  String label, {
  String parent = '',
  String title = 'Название',
  String kind = 'entry',
  int statements = 0,
  int ord = 0,
}) => RefUnit(
  label: label,
  parentLabel: parent,
  title: title,
  path: parent.isEmpty ? label : '$parent/$label',
  depth: parent.isEmpty ? 0 : 1,
  kind: kind,
  answerable: kind != 'group',
  statements: statements,
  ord: ord,
);

RefStatement statement(int id, String unitLabel, String body, {int ord = 0}) =>
    RefStatement(
      id: id,
      unitLabel: unitLabel,
      kind: 'обязательный',
      designation: 'G$id',
      placeRef: 'с. $id',
      body: body,
      ord: ord,
    );

const icd = RefSource(
  slug: 'icd10',
  title: 'МКБ-10',
  unitWord: 'рубрика',
  statementWord: 'критерий',
  edition: 'версия 2019',
  units: 2,
  statements: 2,
  version: 4,
);

void main() {
  group('разбор ответа', () {
    test('незнакомое поле не роняет строку', () {
      final page = parseUnitsPage({
        'units': [
          {'label': 'F32', 'title': 'Депрессивный эпизод', 'сюрприз': 1},
        ],
        'next': '',
        'version': 3,
      });
      expect(page.items.single.label, 'F32');
      expect(page.dropped, 0);
    });

    test('негодная строка отбрасывается и СЧИТАЕТСЯ', () {
      // Молча выброшенная половина справочника выглядит как «столько в нём
      // и было», и объяснить это врачу будет нечем.
      final page = parseStatementsPage({
        'statements': [
          {'id': 1, 'unitLabel': 'F32', 'bodyMd': 'Годное'},
          {'id': 2, 'unitLabel': 'F32'}, // без текста
          'вовсе не объект',
        ],
        'next': '9',
        'version': 3,
      });
      expect(page.items.length, 1);
      expect(page.dropped, 2);
      expect(page.next, '9');
    });

    test('пустой список источников — это пусто, а не отказ', () {
      expect(parseSources({'sources': []}), isEmpty);
      expect(parseSources({}), isEmpty);
    });

    test('слова источника берутся у него, а не придумываются', () {
      final list = parseSources({
        'sources': [
          {
            'slug': 'icd10',
            'title': 'МКБ-10',
            'unitWord': 'рубрика',
            'statementWord': 'критерий',
            'version': 7,
          },
        ],
      });
      expect(list.single.unitWord, 'рубрика');
      expect(list.single.statementWord, 'критерий');
      expect(list.single.version, 7);
    });
  });

  group('хранение', () {
    setUpAll(sqfliteFfiInit);

    late Database db;
    late ReferenceStore store;

    // База закрывается после каждой проверки, и это не уборка ради
    // порядка: sqflite держит открытые базы по пути, а путь у всех
    // «в памяти» один и тот же. Без закрытия следующая проверка получает
    // базу предыдущей — со всем, что та в неё положила, — и проходит или
    // падает по чужим данным.
    setUp(() async {
      db = await openLocalDatabase(
        factory: databaseFactoryFfi,
        path: inMemoryDatabasePath,
      );
      store = ReferenceStore(db);
    });
    tearDown(() => db.close());

    test('выпуск кладётся целиком и читается обратно', () async {
      await store.replace(
        icd,
        [unit('F30-F39', kind: 'group'), unit('F32', parent: 'F30-F39')],
        [statement(1, 'F32', 'Сниженное настроение')],
        syncedAt: 1000,
      );

      expect(await store.versionOf('icd10'), 4);
      expect((await store.children('icd10', '')).single.label, 'F30-F39');
      expect((await store.children('icd10', 'F30-F39')).single.label, 'F32');
      expect(
        (await store.statements('icd10', 'F32')).single.body,
        'Сниженное настроение',
      );
      expect((await store.sources()).single.syncedAt, 1000);
    });

    test('новый выпуск ЗАМЕЩАЕТ прежний, а не дополняет его', () async {
      // Удалённую рубрику досылать нечем, и копия накапливала бы то, чего
      // в источнике уже нет.
      await store.replace(
        icd,
        [unit('F32'), unit('F33')],
        [statement(1, 'F32', 'Первое'), statement(2, 'F33', 'Второе')],
        syncedAt: 1,
      );
      await store.replace(
        icd,
        [unit('F32')],
        [statement(1, 'F32', 'Первое')],
        syncedAt: 2,
      );

      expect((await store.children('icd10', '')).map((u) => u.label), ['F32']);
      expect(await store.statements('icd10', 'F33'), isEmpty);
    });

    test('род единицы доезжает до устройства', () async {
      // По нему рисуется знак группы, и угадывать его по виду метки —
      // тот самый дефект «если это МКБ».
      await store.replace(
        icd,
        [unit('F30-F39', kind: 'group')],
        const [],
        syncedAt: 1,
      );
      final got = await store.unit('icd10', 'F30-F39');
      expect(got!.kind, 'group');
      expect(got.answerable, isFalse);
    });

    test('поиск идёт и по метке, и по названию, метка первой', () async {
      await store.replace(
        icd,
        [
          unit('F41.2', title: 'Смешанное тревожное расстройство', ord: 2),
          unit('F32', title: 'Депрессивный эпизод', ord: 1),
        ],
        const [],
        syncedAt: 1,
      );

      // Строчными буквами: врач набирает как ему удобно, а LIKE в SQLite
      // сворачивает регистр только у латиницы.
      expect((await store.search('icd10', 'депресс')).single.label, 'F32');
      expect((await store.search('icd10', 'ДЕПРЕСС')).single.label, 'F32');
      // Набравший «F3» хочет F32, а не первую попавшуюся рубрику, где эти
      // знаки встретились в середине.
      expect((await store.search('icd10', 'F3')).first.label, 'F32');
    });

    test('знак подстановки из запроса обезврежен', () async {
      // Иначе «%» из опечатки отвечает всем справочником сразу.
      await store.replace(icd, [unit('F32')], const [], syncedAt: 1);
      expect(await store.search('icd10', '%'), isEmpty);
    });

    test('уборка источника уносит и его строки', () async {
      await store.replace(
        icd,
        [unit('F32')],
        [statement(1, 'F32', 'Текст')],
        syncedAt: 1,
      );
      await store.forget('icd10');

      expect(await store.sources(), isEmpty);
      expect(await store.versionOf('icd10'), 0);
      expect(await store.statements('icd10', 'F32'), isEmpty);
    });
  });

  group('закачка', () {
    late FakeServer server;
    late Api api;
    late Database db;
    late ReferenceStore store;
    late ReferenceSync sync;

    setUpAll(sqfliteFfiInit);

    setUp(() async {
      server = await FakeServer.start();
      final tokens = MemoryTokenStore();
      await tokens.write('t-1');
      api = Api(baseUrl: server.origin, appKey: 'k', tokens: tokens);
      db = await openLocalDatabase(
        factory: databaseFactoryFfi,
        path: inMemoryDatabasePath,
      );
      store = ReferenceStore(db);
      sync = ReferenceSync(api, store, now: () => DateTime(2026, 9, 19));
    });

    tearDown(() async {
      api.close();
      await server.stop();
      // Та же ловушка, что и в хранении: непрокрытая база уехала бы в
      // следующую проверку вместе с уже скачанным выпуском, и закачка в
      // ней «пропускалась бы как совпавшая».
      await db.close();
    });

    test('выпуск собирается со всех страниц и ложится в базу', () async {
      server.replies.addAll([
        Reply(200, {
          'units': [
            {'label': 'F32', 'title': 'Депрессивный эпизод', 'kind': 'entry'},
          ],
          'next': 'F32',
          'version': 4,
        }),
        Reply(200, {
          'units': [
            {'label': 'F33', 'title': 'Рекуррентное расстройство'},
          ],
          'next': '',
          'version': 4,
        }),
        Reply(200, {
          'statements': [
            {'id': 1, 'unitLabel': 'F32', 'bodyMd': 'Сниженное настроение'},
          ],
          'next': '',
          'version': 4,
        }),
      ]);

      final report = await sync.pull(icd);

      expect(report.units, 2);
      expect(report.statements, 1);
      expect(report.restarted, 0);
      expect(await store.versionOf('icd10'), 4);
      expect((await store.children('icd10', '')).length, 2);
    });

    test('совпавший выпуск не качается заново', () async {
      await store.replace(icd, [unit('F32')], const [], syncedAt: 1);
      final report = await sync.pull(icd);

      expect(report.skipped, isTrue);
      expect(server.taken, isEmpty);
    });

    test('выпуск, сменившийся посреди обхода, роняет собранное', () async {
      // Копия из двух выпусков выглядит целой, а таковой не является, и
      // заметить это потом нечем.
      server.replies.addAll([
        Reply(200, {
          'units': [
            {'label': 'F32', 'title': 'Было'},
          ],
          'next': 'F32',
          'version': 4,
        }),
        // Вторая страница уже из следующего выпуска.
        Reply(200, {
          'units': [
            {'label': 'F33', 'title': 'Стало'},
          ],
          'next': '',
          'version': 5,
        }),
        // Обход начинается заново и на этот раз доходит до конца.
        Reply(200, {
          'units': [
            {'label': 'F32', 'title': 'Стало'},
          ],
          'next': '',
          'version': 5,
        }),
        Reply(200, {'statements': [], 'next': '', 'version': 5}),
      ]);

      final report = await sync.pull(icd);

      expect(report.restarted, 1);
      expect(report.version, 5);
      expect(await store.versionOf('icd10'), 5);
      expect((await store.unit('icd10', 'F32'))!.title, 'Стало');
    });

    test('вечно меняющийся выпуск кончается отказом словами', () async {
      // Бесконечный цикл врач увидел бы как вечную закачку, и это хуже
      // честного отказа.
      for (var i = 0; i < 20; i++) {
        server.replies.add(
          Reply(200, {
            'units': [
              {'label': 'F3$i', 'title': 'Шатается'},
            ],
            'next': 'F3$i',
            'version': 10 + i,
          }),
        );
      }

      await expectLater(
        sync.pull(icd),
        throwsA(
          isA<ApiFailure>().having(
            (f) => f.message,
            'сообщение',
            contains('обновляется'),
          ),
        ),
      );
      expect(await store.versionOf('icd10'), 0);
    });

    test('отброшенное при закачке считается и доезжает до отчёта', () async {
      server.replies.addAll([
        Reply(200, {
          'units': [
            {'label': 'F32', 'title': 'Годная'},
            {'title': 'Без метки'},
          ],
          'next': '',
          'version': 2,
        }),
        Reply(200, {'statements': [], 'next': '', 'version': 2}),
      ]);

      final report = await sync.pull(icd);
      expect(report.dropped, 1);
      expect(report.units, 1);
    });
  });
}
