/// Лента без сети.
///
/// Ради этого наборы и ставятся на устройство. Пока лента ходила только к
/// серверу, скачанный набор лежал мёртвым грузом: витрина говорила
/// «работает без сети», а в метро врач видел отказ.
library;

import 'package:curator/api/client.dart';
import 'package:curator/cases/feed.dart';
import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_server.dart';

Map<String, Object?> body(String title) => {
  'kind': 'recognise',
  'title': title,
  'answer': 'Верный',
  'segments': [
    {'text': 'Условие задачи', 'statements': <String>[]},
  ],
  // У «узнавания» ответ сверяется с МЕТКОЙ варианта, а не с его текстом:
  // текст врач читает, меткой называется диагноз.
  'options': [
    {'text': 'Первый вариант', 'label': 'Верный'},
    {'text': 'Второй вариант', 'label': 'Неверный'},
  ],
  'explanationMd': 'Разбор',
  'difficulty': 2,
};

Release release(String slug) => Release.tryParse({
  'slug': slug,
  'version': 1,
  'title': 'Набор',
  'releasedAt': '2026-09-19T10:00:00Z',
  'signature': 'подпись',
  'keyId': 'key-1',
  'cases': <Object?>[],
})!;

void main() {
  late FakeServer server;
  late Api api;
  late MemoryPackStore packs;

  setUp(() async {
    server = await FakeServer.start();
    final tokens = MemoryTokenStore();
    await tokens.write('t-42');
    api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
    packs = MemoryPackStore();
  });

  tearDown(() async {
    api.close();
    await server.stop();
  });

  Future<void> install(String slug, List<String> ids) async {
    for (final id in ids) {
      await packs.putCase(slug, id, body('Задача $id'));
    }
    await packs.putRelease(release(slug));
  }

  test('без сети лента берётся из скачанного набора', () async {
    await install('cardio', ['c-01', 'c-02']);
    await server.stop();

    final page = await Feed(api, packs).page(limit: 20);

    expect(page.cases.length, 2);
    expect(page.cases.first.id, 'c-01');
    expect(page.cases.first.title, 'Задача c-01');
  });

  test('отказ сервера набором НЕ подменяется', () async {
    // «Войдите заново» и «нет сети» — разные беды. Подмени мы первую
    // задачами из набора, врач занимался бы, не зная, что его разборы
    // никуда не уходят.
    await install('cardio', ['c-01']);
    server.replies.add(Reply(401, {'error': 'Устройство не опознано'}));

    await expectLater(
      Feed(api, packs).page(limit: 20),
      throwsA(isA<ApiFailure>().having((e) => e.status, 'код', 401)),
    );
  });

  test('без сети и без наборов врачу говорят про сеть', () async {
    // Пустая лента читается как «задачи кончились», а это другая беда с
    // другим лечением.
    await server.stop();

    await expectLater(
      Feed(api, packs).page(limit: 20),
      throwsA(isA<ApiFailure>().having((e) => e.offline, 'без сети', isTrue)),
    );
  });

  test('недокачанный набор в ленту не идёт', () async {
    // Описи нет — значит, набор стоит не целиком, и показывать его куски
    // значит выдавать половину за целое.
    await packs.putCase('cardio', 'c-01', body('Задача'));
    await server.stop();

    await expectLater(
      Feed(api, packs).page(limit: 20),
      throwsA(isA<ApiFailure>()),
    );
  });

  test('лента без сети режется страницами по тому же курсору', () async {
    await install('cardio', ['c-01', 'c-02', 'c-03']);
    await server.stop();

    final feed = Feed(api, packs);
    final first = await feed.page(limit: 2);
    expect([for (final one in first.cases) one.id], ['c-01', 'c-02']);
    // Курсор — номер последней ОТДАННОЙ задачи: сервер продолжает по
    // строгому «больше чем». Отдай мы здесь первую неотданную, на каждой
    // границе страниц терялось бы по задаче, и незаметно.
    expect(first.next, 'c-02');

    final second = await feed.page(after: first.next, limit: 2);
    expect([for (final one in second.cases) one.id], ['c-03']);
    expect(second.next, isEmpty);
  });

  test('задачи из разных наборов идут одной лентой', () async {
    await install('cardio', ['c-01', 'c-03']);
    await install('nevro', ['c-02']);
    await server.stop();

    final page = await Feed(api, packs).page(limit: 20);
    expect([for (final one in page.cases) one.id], ['c-01', 'c-02', 'c-03']);
  });

  test('битая задача считается и не прячется', () async {
    // Молча выброшенная половина ленты выглядит как «задач больше нет», а
    // это разные беды с разным лечением.
    await packs.putCase('cardio', 'c-01', body('Целая'));
    await packs.putCase('cardio', 'c-02', {'kind': 'recognise'});
    await packs.putRelease(release('cardio'));
    await server.stop();

    final page = await Feed(api, packs).page(limit: 20);
    expect(page.cases.length, 1);
    expect(page.dropped, 1);
  });

  test('с сетью лента по-прежнему идёт с сервера', () async {
    // Запасной путь не должен становиться основным: на сервере задач
    // больше, чем в скачанных наборах.
    await install('cardio', ['c-01']);
    server.replies.add(
      Reply(200, {
        'cases': [
          {'id': 's-01', 'body': body('С сервера')},
        ],
        'next': '',
        'version': 7,
      }),
    );

    final page = await Feed(api, packs).page(limit: 20);
    expect(page.cases.single.id, 's-01');
    expect(page.version, 7);
  });
}
