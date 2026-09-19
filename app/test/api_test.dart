import 'dart:convert';
import 'dart:io';

import 'package:curator/api/client.dart';
import 'package:flutter_test/flutter_test.dart';

/// Проверки двери идут против настоящего сервера на петле.
///
/// Поддельный клиент проверил бы только то, что придумал писавший:
/// заголовки, коды ответа и — главное — кодировку он подделал бы вместе с
/// ошибкой. Латинская единица вместо русской буквы не роняет ничего, она
/// просто приезжает другой строкой, и поймать это можно только настоящим
/// сокетом.
class FakeServer {
  FakeServer(this._http);

  final HttpServer _http;
  final List<Taken> taken = [];

  /// Что ответить на следующий запрос. Список, а не одно значение: пачка
  /// проверок ходит по очереди, и каждой нужен свой ответ.
  final List<Reply> replies = [];

  static Future<FakeServer> start() async {
    // 127.0.0.1, а не localhost: localhost на части образов
    // разворачивается в ::1, слушатель сидит на IPv4, и отказ читается как
    // «сервер не поднялся», хотя он поднялся.
    final http = await HttpServer.bind('127.0.0.1', 0);
    final out = FakeServer(http);
    http.listen((request) async {
      final body = await utf8.decoder.bind(request).join();
      out.taken.add(
        Taken(
          method: request.method,
          path: request.uri.path,
          query: request.uri.queryParameters,
          headers: {
            for (final name in ['authorization', 'x-app-key', 'content-type'])
              name: request.headers.value(name) ?? '',
          },
          body: body,
        ),
      );
      final reply = out.replies.isEmpty
          ? Reply(200, {})
          : out.replies.removeAt(0);
      request.response.statusCode = reply.status;
      request.response.headers.contentType = ContentType(
        'application',
        'json',
        charset: 'utf-8',
      );
      request.response.add(utf8.encode(reply.text ?? jsonEncode(reply.body)));
      await request.response.close();
    });
    return out;
  }

  Uri get origin => Uri.parse('http://127.0.0.1:${_http.port}');

  Future<void> stop() => _http.close(force: true);
}

class Taken {
  Taken({
    required this.method,
    required this.path,
    required this.query,
    required this.headers,
    required this.body,
  });

  final String method;
  final String path;
  final Map<String, String> query;
  final Map<String, String> headers;
  final String body;
}

class Reply {
  Reply(this.status, this.body, {this.text});

  final int status;
  final Map<String, dynamic> body;

  /// Ответ текстом мимо JSON — чтобы проверить, что непонятое не
  /// применяется.
  final String? text;
}

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
