import 'dart:convert';

import 'package:curator/api/client.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_server.dart';

void main() {
  late FakeServer server;
  late Api api;
  late MemoryTokenStore tokens;

  setUp(() async {
    server = await FakeServer.start();
    tokens = MemoryTokenStore();
    api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
  });

  tearDown(() async {
    api.close();
    await server.stop();
  });

  test('заведение устройства запоминает токен и шлёт ключ сборки', () async {
    server.replies.add(
      Reply(201, {'token': 't-42', 'accountId': 7, 'deviceId': 9}),
    );

    await api.enroll(platform: 'android', appVersion: '0.1.0');

    expect(await tokens.read(), 't-42');
    final taken = server.taken.single;
    expect(taken.method, 'POST');
    expect(taken.path, '/v1/devices');
    // Ключ сборки, а не токен: заведение происходит до учётной записи.
    expect(taken.headers['x-app-key'], 'app-key-1');
    expect(taken.headers['authorization'], '');
    expect(jsonDecode(taken.body)['platform'], 'android');
  });

  test('заведение без токена в ответе отказывает словами', () async {
    // Пустой токен, принятый за правду, оставил бы приложение навсегда
    // «заведённым» и не способным войти.
    server.replies.add(Reply(201, {'accountId': 7}));

    await expectLater(
      api.enroll(),
      throwsA(isA<ApiFailure>().having((e) => e.message, 'текст', isNotEmpty)),
    );
    expect(await tokens.read(), isNull);
  });

  test('заведённое устройство второй раз не заводится', () async {
    // Второе заведение — это вторая учётная запись и потерянный прогресс
    // при каждом запуске.
    await tokens.write('t-already');
    await api.ensureEnrolled();
    expect(server.taken, isEmpty);
  });

  test('кириллица в теле доезжает целой', () async {
    // Строковое тело HTTP в Dart уезжает latin1, и русский текст приезжает
    // мусором, не уронив ничего. Ловится это только настоящим сокетом.
    await tokens.write('t-42');
    server.replies.add(Reply(200, {'accepted': 1}));

    await api.post('/v1/attempts', {'answer': 'Гипертиреоз, узловой зоб'});

    final arrived =
        jsonDecode(server.taken.single.body) as Map<String, dynamic>;
    expect(arrived['answer'], 'Гипертиреоз, узловой зоб');
  });

  test('кириллица в ответе читается целой', () async {
    await tokens.write('t-42');
    server.replies.add(Reply(200, {'title': 'Первые шаги'}));

    final reply = await api.get('/v1/signs');
    expect(reply['title'], 'Первые шаги');
  });

  test('запрос с токеном несёт его заголовком', () async {
    await tokens.write('t-42');
    server.replies.add(Reply(200, {'cases': []}));

    await api.get('/v1/cases', query: {'limit': '20'});

    final taken = server.taken.single;
    expect(taken.headers['authorization'], 'Bearer t-42');
    expect(taken.query['limit'], '20');
  });

  test('запрос без токена отказывает до похода в сеть', () async {
    // Поход без токена вернулся бы «устройство не опознано», и врач пошёл
    // бы переустанавливать исправное приложение.
    await expectLater(api.get('/v1/cases'), throwsA(isA<ApiFailure>()));
    expect(server.taken, isEmpty);
  });

  test('отказ сервера приезжает его словами, а не нашими', () async {
    await tokens.write('t-42');
    server.replies.add(
      Reply(402, {
        'error':
            'Этот набор пока не открыт. Купите его или подписку — и он появится',
      }),
    );

    await expectLater(
      api.get('/v1/packs/cardio/cases'),
      throwsA(
        isA<ApiFailure>()
            .having((e) => e.status, 'код', 402)
            .having((e) => e.message, 'текст', contains('Купите')),
      ),
    );
  });

  test('отказ без внятного текста подменяется нашим', () async {
    await tokens.write('t-42');
    server.replies.add(Reply(500, {}));

    await expectLater(
      api.get('/v1/cases'),
      throwsA(isA<ApiFailure>().having((e) => e.message, 'текст', isNotEmpty)),
    );
  });

  test('непонятный ответ не применяется', () async {
    // Подставь мы здесь пустой ответ — и работа приняла бы его за правду,
    // записав пустоту как результат.
    await tokens.write('t-42');
    server.replies.add(Reply(200, {}, text: 'совсем не json'));

    await expectLater(api.get('/v1/cases'), throwsA(isA<ApiFailure>()));
  });

  test('негодный токен отказывает словами, а не падением', () async {
    // Токен приезжает с сервера и хранится на устройстве, а заголовок HTTP
    // принимает только ASCII. Испорченное хранилище не должно ронять
    // приложение исключением чужого рода: врач увидел бы не отказ, а
    // остановку.
    await tokens.write('т-порченый');

    await expectLater(
      api.get('/v1/cases'),
      throwsA(isA<ApiFailure>().having((e) => e.message, 'текст', isNotEmpty)),
    );
  });

  test('молчащий сервер читается как отсутствие связи', () async {
    // «Нет сети» — это «попробуйте позже», а отказ сервера может значить
    // «войдите заново». Путать их значит отправлять врача
    // переустанавливать приложение вместо того, чтобы выйти из метро.
    await tokens.write('t-42');
    await server.stop();

    await expectLater(
      api.get('/v1/cases'),
      throwsA(isA<ApiFailure>().having((e) => e.offline, 'без сети', isTrue)),
    );
    // Чтобы tearDown не снимал снятое дважды.
    server = await FakeServer.start();
  });
}
