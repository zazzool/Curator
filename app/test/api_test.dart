import 'dart:convert';

import 'package:curator/account/account.dart';
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

  test('возврат доступа идёт с ключом сборки и без токена', () async {
    // Врач приходит сюда ровно тогда, когда токена у него уже нет: телефон
    // сменился, приложение поставлено заново. Уйди запрос с заголовком
    // входа — сервер отказал бы «устройство не опознано», и врач пошёл бы
    // переустанавливать исправное приложение.
    await tokens.write('старый-токен');
    server.replies.add(Reply(202, {'sent': true}));

    await api.startRecovery('  VN@Example.COM  ');

    final taken = server.taken.single;
    expect(taken.path, '/v1/recovery');
    expect(taken.headers['x-app-key'], 'app-key-1');
    expect(taken.headers['authorization'], '');
    // Пробелы срезаются здесь: уехавший на сервер пробел вернулся бы
    // адресом с пробелом, и врач вспоминал бы не тот адрес.
    expect(jsonDecode(taken.body)['email'], 'VN@Example.COM');
  });

  test('вернувшийся доступ перезаписывает токен устройства', () async {
    await tokens.write('старый-токен');
    server.replies.add(
      Reply(201, {'token': 'т-новый', 'accountId': 7, 'deviceId': 11}),
    );

    await api.confirmRecovery(
      'vn@example.com',
      ' 123456 ',
      platform: 'android',
    );

    expect(await tokens.read(), 'т-новый');
    final body = jsonDecode(server.taken.single.body);
    expect(body['code'], '123456');
    expect(body['email'], 'vn@example.com');
  });

  test(
    'возврат доступа без токена в ответе отказывает и НЕ трогает прежний',
    () async {
      // Пустой токен, принятый за правду, стёр бы вход, который работал:
      // врач остался бы и без прежней записи, и без вернувшейся.
      await tokens.write('старый-токен');
      server.replies.add(Reply(201, {'accountId': 7}));

      await expectLater(
        api.confirmRecovery('vn@example.com', '123456'),
        throwsA(
          isA<ApiFailure>().having((e) => e.message, 'текст', isNotEmpty),
        ),
      );
      expect(await tokens.read(), 'старый-токен');
    },
  );

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

  test('погасший сервер читается как отсутствие связи', () async {
    // «Нет сети» — это «попробуйте позже», а отказ сервера может значить
    // «войдите заново». Путать их значит отправлять врача
    // переустанавливать приложение вместо того, чтобы выйти из метро.
    //
    // Проверка называется погасшим, а не молчащим, и это исправление:
    // прежде она называлась молчащим сервером, а гасила его. Система
    // отказывает в соединении с погашенным мгновенно, и проверка
    // проходила у клиента, не имевшего ни одного срока. Молчащий —
    // отдельная проверка ниже.
    await tokens.write('t-42');
    await server.stop();

    await expectLater(
      api.get('/v1/cases'),
      throwsA(isA<ApiFailure>().having((e) => e.offline, 'без сети', isTrue)),
    );
    // Чтобы tearDown не снимал снятое дважды.
    server = await FakeServer.start();
  });

  test('молчащий сервер упирается в срок, а не висит вечно', () async {
    // Портал гостиничного или больничного Wi-Fi принимает соединение и не
    // отвечает ни байтом. Без срока запрос не завершается никогда и не
    // бросает ничего: первый запуск оставался кружком на весь экран, и
    // выйти из него было нечем, кроме убийства приложения.
    await tokens.write('t-42');
    server.silent = true;

    final impatient = Api(
      baseUrl: server.origin,
      appKey: 'app-key-1',
      tokens: tokens,
      replyTimeout: const Duration(milliseconds: 300),
    );
    addTearDown(impatient.close);

    final started = DateTime.now();
    await expectLater(
      impatient.get('/v1/cases'),
      // Тем же отказом, что и оборванная связь: врачу делать в обоих
      // случаях одно и то же, а «истёк срок ожидания» объясняло бы наше
      // устройство, а не его положение.
      throwsA(isA<ApiFailure>().having((e) => e.offline, 'без сети', isTrue)),
    );
    expect(
      DateTime.now().difference(started),
      lessThan(const Duration(seconds: 10)),
      reason: 'срок не сработал: запрос ждал дольше, чем ему отведено',
    );
  });

  group('запись врача', () {
    // Обрезка живёт здесь, а не на экране: экран отдаёт набранное как
    // есть, и проверка экрана на подменённой записи проверила бы саму
    // подмену, а не код. Пробел, уехавший на сервер, вернулся бы оттуда
    // именем — и адресом — с пробелом.
    test('имя и адрес уходят обрезанными', () async {
      await tokens.write('t-1');
      final account = Account(api);

      server.replies.add(Reply(200, {}));
      await account.rename('  Пётр Петрович  ');
      expect(
        jsonDecode(server.taken.last.body)['displayName'],
        'Пётр Петрович',
      );

      server.replies.add(Reply(202, {'sent': true}));
      await account.startBind('  vn@example.com  ');
      expect(jsonDecode(server.taken.last.body)['email'], 'vn@example.com');
    });

    test('отозванный токен стирается, и отказ говорит об этом', () async {
      // Самое дорогое место двери. `ensureEnrolled` видит непустой токен
      // и не заводит устройство заново НИКОГДА: не сотри мы отозванный
      // токен — и каждый экран отказывал бы «устройство не опознано»
      // до переустановки приложения, то есть до потери всего, что не
      // привязано к почте.
      //
      // Токен латиницей, и это не мелочь: заголовок HTTP принимает только
      // ASCII, кириллический токен роняет `FormatException` ещё до похода
      // к серверу — и проверка сверяла бы совсем другой путь двери.
      await tokens.write('revoked-1');
      server.replies.add(Reply(401, {'error': 'Устройство не опознано'}));

      ApiFailure? failure;
      try {
        await api.get('/v1/feed');
      } on ApiFailure catch (error) {
        failure = error;
      }

      expect(failure, isNotNull);
      expect(failure!.status, 401);
      expect(
        failure.lostDevice,
        isTrue,
        reason: 'экрану нечем предложить возврат записи',
      );
      expect(
        await tokens.read(),
        isNull,
        reason: 'приложение осталось окирпиченным навсегда',
      );
    });

    test('401 без токена токена не трогает', () async {
      // Возврат доступа по почте отвечает 401 на неподошедший код. Сотри
      // мы здесь что-нибудь — врач, ошибшийся в коде, терял бы заодно и
      // работающее устройство, с которого этот возврат затеял.
      await tokens.write('рабочий');
      server.replies.add(Reply(401, {'error': 'Код не подошёл'}));

      await expectLater(
        api.confirmRecovery('vn@example.com', '000000'),
        throwsA(
          isA<ApiFailure>().having((f) => f.lostDevice, 'lostDevice', isFalse),
        ),
      );
      expect(await tokens.read(), 'рабочий');
    });

    test('привязанный адрес берётся из ответа, а не из набранного', () async {
      // Сервер сводит адрес к одному виду — снимает регистр и пробелы.
      // Покажи мы набранное, врач запомнил бы не тот адрес, по которому
      // потом будет возвращать доступ.
      await tokens.write('t-1');
      server.replies.add(Reply(200, {'email': 'vn@example.com'}));

      expect(await Account(api).confirmBind(' 123456 '), 'vn@example.com');
      expect(jsonDecode(server.taken.last.body)['code'], '123456');
    });
  });
}
