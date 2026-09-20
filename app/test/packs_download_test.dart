/// Проверки закачки набора.
///
/// Здесь проверяется не то, что задачи доезжают, а то, что подменённые не
/// доезжают. Закачка без этих проверок выглядит работающей ровно так же.
library;

import 'dart:convert';

import 'package:cryptography/cryptography.dart';
import 'package:curator/api/client.dart';
import 'package:curator/packs/download.dart';
import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_server.dart';

/// Набор, подписанный на месте настоящим ключом.
///
/// Подпись здесь не подделывается: проверка с подделанной подписью
/// проверяла бы подделку, а не сверку.
class Signed {
  Signed(this.release, this.keys, this.bodies);

  final Release release;
  final TrustedKeys keys;
  final Map<String, Object?> bodies;
}

Future<Signed> signed({
  int count = 3,
  String slug = 'primery',
  int version = 3,
}) async {
  final pair = await Ed25519().newKeyPair();
  final public = await pair.extractPublicKey();

  final bodies = <String, Object?>{};
  final cases = <Map<String, Object?>>[];
  for (var i = 0; i < count; i++) {
    // Номера с ведущим нулём: сервер отдаёт по возрастанию номера, и
    // курсор ходит по тому же порядку.
    final id = 'c-${i.toString().padLeft(2, '0')}';
    final body = {'text': 'Условие $i', 'ord': i};
    bodies[id] = body;
    cases.add({'id': id, 'ord': i, 'hash': await hashBody(body)});
  }

  final unsigned = Release.tryParse({
    'slug': slug,
    'version': version,
    'title': 'Пример & образец',
    'releasedAt': '2026-09-19T10:00:00Z',
    'signature': 'покане',
    'keyId': 'key-1',
    'cases': cases,
  })!;
  final signature = await Ed25519().sign(
    utf8.encode(unsigned.canonical),
    keyPair: pair,
  );
  final release = Release.tryParse({
    'slug': unsigned.slug,
    'version': unsigned.version,
    'title': unsigned.title,
    'releasedAt': unsigned.releasedAt,
    'signature': base64.encode(signature.bytes),
    'keyId': 'key-1',
    'cases': cases,
  })!;

  return Signed(
    release,
    TrustedKeys.parse('key-1:${base64.encode(public.bytes)}'),
    bodies,
  );
}

/// Ответ описи, как его шлёт сервер.
Map<String, Object?> releaseReply(Release release) => {
  'slug': release.slug,
  'version': release.version,
  'title': release.title,
  'releasedAt': release.releasedAt,
  'signature': release.signature,
  'keyId': release.keyId,
  'cases': [
    for (final one in release.cases)
      {'id': one.id, 'ord': one.ord, 'hash': one.hash},
  ],
};

/// Страница задач, как её шлёт сервер.
Map<String, Object?> page(
  Map<String, Object?> bodies,
  List<String> ids, {
  String next = '',
}) => {
  'cases': [
    for (final id in ids) {'id': id, 'body': bodies[id]},
  ],
  'next': next,
};

void main() {
  late FakeServer server;
  late Api api;
  late MemoryPackStore store;

  setUp(() async {
    server = await FakeServer.start();
    final tokens = MemoryTokenStore();
    await tokens.write('t-42');
    api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
    store = MemoryPackStore();
  });

  tearDown(() async {
    api.close();
    await server.stop();
  });

  test('целый набор ложится на устройство вместе с описью', () async {
    final one = await signed();
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, one.bodies.keys.toList())));

    final out = await Download(api, store, one.keys).run('primery');

    expect(out.saved, 3);
    expect(await store.have('primery'), ['c-00', 'c-01', 'c-02']);
    expect((await store.release('primery'))!.version, 3);
    expect(await store.caseBody('primery', 'c-01'), one.bodies['c-01']);
  });

  test('подменённая задача не ложится, и набор не ставится', () async {
    // Ради этого всё и написано: отпечаток из подписанной описи —
    // единственное, что отличает задачу от подменённой по дороге.
    final one = await signed();
    final spoiled = {...one.bodies};
    spoiled['c-01'] = {'text': 'Подменили', 'ord': 1};
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(spoiled, spoiled.keys.toList())));

    await expectLater(
      Download(api, store, one.keys).run('primery'),
      throwsA(isA<ContentFailure>()),
    );
    // Опись не легла — значит, набор не выдаст себя за целый.
    expect(await store.release('primery'), isNull);
  });

  test('подменённая опись не доходит до задач вовсе', () async {
    // Сверка подписи стоит ДО первой скачанной задачи: иначе подменённый
    // набор успел бы лечь на устройство.
    final one = await signed();
    final reply = releaseReply(one.release);
    (reply['cases'] as List)[0] = {'id': 'c-00', 'ord': 0, 'hash': 'sha256:ff'};
    server.replies.add(Reply(200, reply));

    await expectLater(
      Download(api, store, one.keys).run('primery'),
      throwsA(isA<SignatureFailure>()),
    );
    // Второго запроса не было: за описью никто не пошёл.
    expect(server.taken.length, 1);
    expect(await store.have('primery'), isEmpty);
  });

  test('оборванная закачка продолжается, а не начинается заново', () async {
    // Метро кончается раньше, чем набор.
    final one = await signed(count: 4);
    final ids = one.bodies.keys.toList();
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(
      Reply(200, page(one.bodies, ids.sublist(0, 2), next: 'c-01')),
    );
    server.replies.add(Reply(500, {}));

    await expectLater(
      Download(api, store, one.keys).run('primery'),
      throwsA(isA<ApiFailure>()),
    );
    expect(await store.have('primery'), ['c-00', 'c-01']);
    expect(await store.release('primery'), isNull);

    server.taken.clear();
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, ids.sublist(2))));

    final out = await Download(api, store, one.keys).run('primery');
    expect(out.saved, 2, reason: 'докачаны только недостающие');
    // Курсор ушёл с конца уже лежащего, а не с начала набора.
    expect(server.taken[1].query['after'], 'c-01');
    expect(await store.release('primery'), isNotNull);
  });

  test('дыра в лежащем докачивается, а не перепрыгивается', () async {
    // Состав между выпусками меняется, и в лежащем может оказаться
    // пропуск. Курсор по наибольшему лежащему перепрыгнул бы его, и набор
    // не докачался бы никогда, сколько ни повторяй.
    final one = await signed(count: 3);
    await store.putCase('primery', 'c-02', one.bodies['c-02']);

    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, ['c-00', 'c-01', 'c-02'])));

    final out = await Download(api, store, one.keys).run('primery');

    expect(
      server.taken[1].query['after'],
      isNull,
      reason: 'сплошного начала нет — качаем с начала',
    );
    expect(out.saved, 2);
    expect(await store.have('primery'), ['c-00', 'c-01', 'c-02']);
  });

  test('недосчитанный набор не ставится', () async {
    // Набор с дырой хуже его отсутствия: врач увидит пропуски и решит, что
    // задач просто нет.
    final one = await signed(count: 3);
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, ['c-00'])));

    await expectLater(
      Download(api, store, one.keys).run('primery'),
      throwsA(isA<ContentFailure>()),
    );
    expect(await store.release('primery'), isNull);
    // Приехавшее не выброшено: следующая попытка продолжит с него.
    expect(await store.have('primery'), ['c-00']);
  });

  test('закрытый набор отказывает словами сервера', () async {
    // Врач должен понять, что набор надо купить, а не что приложение
    // сломалось.
    final one = await signed();
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(
      Reply(402, {
        'error':
            'Этот набор пока не открыт. Купите его или подписку — и он появится',
      }),
    );

    await expectLater(
      Download(api, store, one.keys).run('primery'),
      throwsA(
        isA<ApiFailure>()
            .having((e) => e.status, 'код', 402)
            .having((e) => e.message, 'текст', contains('Купите')),
      ),
    );
  });

  test('номер задачи с выходом из каталога не сохраняется', () async {
    // Номер приезжает с сервера, а сервер здесь — как раз та сторона,
    // которой мы не верим на слово.
    expect(safeName('../../secrets'), isFalse);
    expect(safeName('c-01'), isTrue);
    expect(safeName(''), isFalse);
    expect(safeName('..'), isFalse);
    expect(safeName('задача'), isFalse);
    expect(() => store.putCase('primery', '../x', {}), throwsArgumentError);
  });

  test('задача не из описи не берётся, но набор ставится', () async {
    // Состав мог смениться между описью и этой страницей: лишнее просто
    // не берётся.
    final one = await signed();
    final ids = one.bodies.keys.toList();
    final extra = {
      ...one.bodies,
      'c-99': {'text': 'Лишняя'},
    };
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(extra, [...ids, 'c-99'])));

    final out = await Download(api, store, one.keys).run('primery');

    expect(out.saved, 3);
    expect(await store.have('primery'), ['c-00', 'c-01', 'c-02']);
  });
  test('опись не того набора не принимается', () async {
    // Подпись метку покрывает, значит подделать её нельзя. Но верно
    // подписанный выпуск набора А приезжал в ответ на просьбу о наборе Б
    // и принимался: хватило бы ошибки сервера, переименования или
    // псевдонима. Задачи легли бы под запрошенной меткой, опись под
    // пришедшей, и набор навсегда остался бы «не установленным» — врач
    // качал бы его снова и снова, а работа без сети не включилась бы.
    final one = await signed(slug: 'primery');
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, one.bodies.keys.toList())));

    await expectLater(
      Download(api, store, one.keys).run('drugoy'),
      throwsA(isA<ContentFailure>()),
    );
    // Ни под запрошенной меткой, ни под пришедшей ничего не легло.
    expect(await store.release('drugoy'), isNull);
    expect(await store.release('primery'), isNull);
    expect(await store.have('drugoy'), isEmpty);
    expect(await store.have('primery'), isEmpty);
  });

  test('выпуск постарше не ложится поверх установленного', () async {
    // Старый выпуск — это старые ответы. Сравнение версий в витрине есть,
    // но только ДЛЯ ПОКАЗА: оно решает, рисовать ли «Обновить», и ничего
    // не запрещает. Верно подписанный older выпуск лёг бы поверх нового
    // молча.
    final fresh = await signed(version: 5);
    server.replies.add(Reply(200, releaseReply(fresh.release)));
    server.replies.add(
      Reply(200, page(fresh.bodies, fresh.bodies.keys.toList())),
    );
    await Download(api, store, fresh.keys).run('primery');
    expect((await store.release('primery'))!.version, 5);

    final older = await signed(version: 4);
    server.replies.add(Reply(200, releaseReply(older.release)));
    server.replies.add(
      Reply(200, page(older.bodies, older.bodies.keys.toList())),
    );
    final out = await Download(api, store, older.keys).run('primery');

    // Отказа нет намеренно: ставить нечего, и объявить это отказом
    // значило бы назвать бедой то, что у врача уже всё есть.
    expect(out.saved, 0);
    expect((await store.release('primery'))!.version, 5);
    // За описью не пошли вовсе: страница задач осталась неотданной.
    expect(server.replies, isNotEmpty);
  });

  test('тот же выпуск поверх себя не отказывает', () async {
    // Иначе запрет отката запретил бы и обычную перепроверку: витрина
    // дёргает закачку, чтобы добрать недостающее, и равная версия — это
    // ровно тот случай.
    final one = await signed(version: 5);
    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, one.bodies.keys.toList())));
    await Download(api, store, one.keys).run('primery');

    server.replies.add(Reply(200, releaseReply(one.release)));
    server.replies.add(Reply(200, page(one.bodies, one.bodies.keys.toList())));
    final out = await Download(api, store, one.keys).run('primery');

    expect(out.total, 3);
    expect((await store.release('primery'))!.version, 5);
  });
}
