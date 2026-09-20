/// Дверь приложения к серверу.
///
/// # Кириллица в теле запроса
///
/// Строковое тело HTTP в Dart уезжает latin1, и русский текст приезжает
/// мусором — причём отказа при этом нет: сервер получает строку, просто
/// другую. Поэтому тело здесь всегда байты (`utf8.encode`), и ответ тоже
/// читается байтами (`utf8.decode`), а не готовой строкой.
///
/// # Клиент берётся готовым, а не заводится своим
///
/// Сеть — это то, чего в проверке быть не должно: проверка, ходящая
/// наружу, красна не тогда, когда сломан код. Здесь нет ни одной
/// зависимости сверх набора: `HttpClient` из `dart:io` подменяется в
/// проверках настоящим сервером на `127.0.0.1` — и это лучше поддельного
/// клиента, потому что ловит и кодировку, и заголовки, и коды ответа.
///
/// # Отказ говорит словами
///
/// Текст отказа пишется по-русски и говорит, что делать: его читает врач,
/// а не программист. Сервер такие тексты уже шлёт полем `error`, и здесь
/// они берутся оттуда, а придумываются только там, где сервер не ответил
/// вовсе.
library;

import 'dart:convert';
import 'dart:io';

/// Отказ двери: то, что можно показать врачу.
class ApiFailure implements Exception {
  ApiFailure(this.message, {this.status = 0, this.offline = false});

  /// Текст по-русски, говорящий, что делать.
  final String message;

  /// Код ответа. Ноль значит, что ответа не было вовсе.
  final int status;

  /// Сети не было. Отличается от отказа сервера намеренно: «нет сети» —
  /// это «попробуйте позже», а отказ сервера может значить «войдите
  /// заново», и путать их значит отправлять врача переустанавливать
  /// приложение вместо того, чтобы выйти из метро.
  final bool offline;

  @override
  String toString() => message;
}

/// Где взять и куда положить токен устройства.
///
/// Заведено доводом, а не полем: хранилище на устройстве и хранилище в
/// проверке — разные вещи, а дверь про эту разницу знать не должна.
abstract class TokenStore {
  Future<String?> read();
  Future<void> write(String token);
  Future<void> clear();
}

/// Хранилище в памяти. Для проверок и для первого запуска до записи.
class MemoryTokenStore implements TokenStore {
  String? _token;

  @override
  Future<String?> read() async => _token;

  @override
  Future<void> write(String token) async => _token = token;

  @override
  Future<void> clear() async => _token = null;
}

/// Дверь к `/v1`.
class Api {
  Api({
    required this.baseUrl,
    required this.appKey,
    required this.tokens,
    HttpClient? client,
  }) : _client = client ?? HttpClient();

  /// Адрес контура. Задаётся снаружи и одним местом: адрес, вписанный
  /// литералом во второй файл, расходится молча.
  final Uri baseUrl;

  /// Ключ программы: «какая сборка стучится», а не «кто стучится». Им
  /// закрыто заведение устройства — без ключа первый же любопытный завёл
  /// бы миллион учётных записей.
  final String appKey;

  final TokenStore tokens;
  final HttpClient _client;

  void close() => _client.close(force: true);

  /// Заводит устройство и запоминает токен.
  ///
  /// Токен отдаётся ровно здесь и больше нигде: дальше в базе лежит только
  /// отпечаток, и «напомнить токен» невозможно by design. Потерян токен —
  /// потеряна запись, поэтому пишется он до того, как о нём узнает
  /// остальное приложение.
  Future<void> enroll({String? platform, String? appVersion}) async {
    final body = await _send(
      'POST',
      '/v1/devices',
      headers: {'X-App-Key': appKey},
      body: {'platform': ?platform, 'appVersion': ?appVersion},
      authorized: false,
    );
    final token = body['token'];
    if (token is! String || token.isEmpty) {
      throw ApiFailure('Не вышло начать. Попробуйте ещё раз');
    }
    await tokens.write(token);
  }

  /// Заводит устройство, если оно ещё не заведено.
  ///
  /// Первый запуск не спрашивает у врача ничего: спрашивать имя у
  /// человека, который ещё не понял, что ему предлагают, — верный способ
  /// его потерять.
  Future<void> ensureEnrolled({String? platform, String? appVersion}) async {
    final token = await tokens.read();
    if (token != null && token.isNotEmpty) return;
    await enroll(platform: platform, appVersion: appVersion);
  }

  /// Просит код возврата доступа на привязанный адрес.
  ///
  /// Без токена и с ключом программы: врач приходит сюда ровно тогда,
  /// когда токена у него уже нет — телефон сменился, приложение поставлено
  /// заново.
  Future<void> startRecovery(String email) async {
    await _send(
      'POST',
      '/v1/recovery',
      headers: {'X-App-Key': appKey},
      body: {'email': email.trim()},
      authorized: false,
    );
  }

  /// Возвращает доступ по коду и запоминает новый токен.
  ///
  /// Токен пишется тем же путём, что и при заведении устройства: сервер
  /// отдаёт его ровно один раз, и «напомнить» его нельзя. Прежний токен
  /// перезаписывается — на новом устройстве его всё равно нет, а на том же
  /// самом врач получил бы два входа в одну запись и один потерял бы
  /// молча.
  Future<void> confirmRecovery(
    String email,
    String code, {
    String? platform,
    String? appVersion,
  }) async {
    final body = await _send(
      'POST',
      '/v1/recovery/confirm',
      headers: {'X-App-Key': appKey},
      body: {
        'email': email.trim(),
        'code': code.trim(),
        'platform': ?platform,
        'appVersion': ?appVersion,
      },
      authorized: false,
    );
    final token = body['token'];
    if (token is! String || token.isEmpty) {
      throw ApiFailure('Не вышло вернуть доступ. Попробуйте ещё раз');
    }
    await tokens.write(token);
  }

  Future<Map<String, dynamic>> get(String path, {Map<String, String>? query}) =>
      _send('GET', path, query: query);

  Future<Map<String, dynamic>> post(String path, Map<String, dynamic> body) =>
      _send('POST', path, body: body);

  Future<Map<String, dynamic>> put(String path, Map<String, dynamic> body) =>
      _send('PUT', path, body: body);

  Future<Map<String, dynamic>> _send(
    String method,
    String path, {
    Map<String, String>? query,
    Map<String, String>? headers,
    Map<String, dynamic>? body,
    bool authorized = true,
  }) async {
    final url = baseUrl.replace(
      path: '${baseUrl.path}$path',
      queryParameters: query,
    );

    HttpClientResponse response;
    List<int> raw;
    try {
      final request = await _client.openUrl(method, url);
      request.headers.set(HttpHeaders.acceptHeader, 'application/json');
      headers?.forEach(request.headers.set);

      if (authorized) {
        final token = await tokens.read();
        if (token == null || token.isEmpty) {
          // Отдельный отказ, а не поход без токена: поход без токена
          // вернулся бы «устройство не опознано», и врач пошёл бы
          // переустанавливать исправное приложение.
          throw ApiFailure('Устройство ещё не заведено');
        }
        request.headers.set(HttpHeaders.authorizationHeader, 'Bearer $token');
      }
      if (body != null) {
        request.headers.contentType = ContentType(
          'application',
          'json',
          charset: 'utf-8',
        );
        // Байтами, а не строкой: строка уедет latin1, и русский текст
        // приедет мусором, не уронив ничего.
        request.add(utf8.encode(jsonEncode(body)));
      }

      response = await request.close();
      raw = await response.fold<List<int>>(
        [],
        (all, chunk) => all..addAll(chunk),
      );
    } on ApiFailure {
      rethrow;
    } on SocketException {
      throw ApiFailure('Нет связи с сервером. Попробуйте позже', offline: true);
    } on HttpException {
      throw ApiFailure('Нет связи с сервером. Попробуйте позже', offline: true);
    } on FormatException {
      // Заголовок HTTP принимает только ASCII, а токен приезжает с сервера
      // и живёт на устройстве. Испорченное хранилище обязано дать отказ, а
      // не исключение чужого рода: второе врач увидит не как отказ, а как
      // остановку приложения.
      throw ApiFailure('Устройство не опознано. Войдите заново');
    }

    final text = utf8.decode(raw, allowMalformed: true);
    Map<String, dynamic>? parsed;
    if (text.trim().isNotEmpty) {
      try {
        final decoded = jsonDecode(text);
        if (decoded is Map<String, dynamic>) parsed = decoded;
      } on FormatException {
        // Непонятое не применяется: подставь мы здесь пустой ответ, и
        // работа приняла бы его за правду и записала пустоту как
        // результат.
        throw ApiFailure(
          'Сервер ответил непонятно',
          status: response.statusCode,
        );
      }
    }

    if (response.statusCode < 200 || response.statusCode >= 300) {
      final said = parsed?['error'];
      throw ApiFailure(
        said is String && said.isNotEmpty
            ? said
            : 'Сервер отказал. Попробуйте позже',
        status: response.statusCode,
      );
    }
    return parsed ?? <String, dynamic>{};
  }
}
