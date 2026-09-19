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

Future<Signed> signed({int count = 3}) async {
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
    'slug': 'primery',
    'version': 3,
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
}
