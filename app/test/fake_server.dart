import 'dart:convert';
import 'dart:io';

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

  /// Молчать: принять соединение, прочитать запрос и не ответить ничего.
  ///
  /// Это не то же самое, что погасший сервер, и разница — вся суть.
  /// Погашенный отказывает в соединении мгновенно, и проверка на нём
  /// проходит даже у клиента без единого срока. Молчащий не отказывает
  /// никогда: так ведёт себя портал гостиничного или больничного Wi-Fi, и
  /// именно на нём приложение зависало кружком на весь экран.
  bool silent = false;

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
      if (out.silent) {
        // Ответ не закрывается намеренно: соединение остаётся открытым, и
        // ждать клиенту нечего до самого своего срока.
        return;
      }
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
