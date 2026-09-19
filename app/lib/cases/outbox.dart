/// Очередь разборов: то, что врач решил, но сервер ещё не получил.
///
/// # Разбор обязан лечь, даже если сети нет
///
/// Врач занимается в метро. Пошли мы попытку сразу и потеряй её на
/// обрыве — пропал бы вечер работы, и узнал бы он об этом по несошедшемуся
/// уровню. Поэтому попытка сперва ложится в очередь, а уходит пачкой, и
/// очередь переживает перезапуск.
///
/// # Ключ повторности выдаёт устройство
///
/// Не сервер: придуманный сервером ключ не пережил бы обрыва ровно в тот
/// момент, ради которого он заведён. Одна и та же попытка, посланная
/// дважды, ляжет один раз — иначе она испортит и прогресс, и решаемость,
/// причём незаметно: числа останутся правдоподобными.
///
/// # Очередь живёт в настройках, а не в местной базе
///
/// Местной базы пока нет. Записи здесь маленькие и недолгие — десятки
/// строк между двумя выходами в сеть, — и настройки их держат. Когда
/// появится местная база, очередь переедет в неё: это записано здесь, а
/// не оставлено на память.
library;

import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import '../api/client.dart';

/// Разбор, ожидающий отправки.
class PendingAttempt {
  const PendingAttempt({
    required this.caseId,
    required this.correct,
    required this.answer,
    required this.mode,
    required this.spentMs,
    required this.idemKey,
    required this.happenedAt,
  });

  final String caseId;
  final bool correct;
  final String answer;
  final String mode;
  final int spentMs;
  final String idemKey;

  /// Когда это было на устройстве, а не когда доехало: время доставки к
  /// обучению отношения не имеет.
  final DateTime happenedAt;

  Map<String, dynamic> toJson() => {
    'caseId': caseId,
    'correct': correct,
    'answer': answer,
    'mode': mode,
    'spentMs': spentMs,
    'idemKey': idemKey,
    'happenedAt': happenedAt.toUtc().toIso8601String(),
  };

  static PendingAttempt? tryParse(Map<String, dynamic> row) {
    final caseId = row['caseId'];
    final idemKey = row['idemKey'];
    final happenedAt = DateTime.tryParse(
      row['happenedAt'] is String ? row['happenedAt'] as String : '',
    );
    if (caseId is! String || caseId.isEmpty) return null;
    if (idemKey is! String || idemKey.isEmpty || happenedAt == null) {
      return null;
    }
    return PendingAttempt(
      caseId: caseId,
      correct: row['correct'] == true,
      answer: row['answer'] is String ? row['answer'] as String : '',
      mode: row['mode'] is String ? row['mode'] as String : '',
      spentMs: row['spentMs'] is int ? row['spentMs'] as int : 0,
      idemKey: idemKey,
      happenedAt: happenedAt,
    );
  }
}

/// Хранилище очереди.
abstract class OutboxStore {
  Future<List<Map<String, dynamic>>> read();
  Future<void> write(List<Map<String, dynamic>> rows);
}

/// Очередь в настройках устройства.
class PrefsOutboxStore implements OutboxStore {
  static const _key = 'attempts.outbox';

  @override
  Future<List<Map<String, dynamic>>> read() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_key);
    if (raw == null || raw.isEmpty) return [];
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! List) return [];
      return decoded.whereType<Map<String, dynamic>>().toList();
    } on FormatException {
      // Испорченная очередь отбрасывается целиком, а не разбирается по
      // кускам: половина очереди — это половина вечера, и выдать её за
      // целую хуже, чем признать потерю.
      return [];
    }
  }

  @override
  Future<void> write(List<Map<String, dynamic>> rows) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, jsonEncode(rows));
  }
}

/// Очередь в памяти — для проверок.
class MemoryOutboxStore implements OutboxStore {
  List<Map<String, dynamic>> _rows = [];

  @override
  Future<List<Map<String, dynamic>>> read() async => List.of(_rows);

  @override
  Future<void> write(List<Map<String, dynamic>> rows) async =>
      _rows = List.of(rows);
}

/// Что стало с попыткой отправки.
class Flushed {
  const Flushed({required this.sent, required this.left, this.failure});

  final int sent;
  final int left;

  /// Отказ, если он был. Очередь при отказе не трогается.
  final ApiFailure? failure;
}

/// Очередь разборов.
class Outbox {
  Outbox(this._api, this._store);

  final Api _api;
  final OutboxStore _store;

  /// Сколько разборов уходит одной посылкой.
  ///
  /// Сервер принимает до пятисот и отказывает на большем, а не обрезает
  /// молча: обрезанная пачка означает потерянный разбор, и врач об этом не
  /// узнает. Здесь потолок тот же, и посылка режется на пачки.
  static const batch = 500;

  Future<void> add(PendingAttempt attempt) async {
    final rows = await _store.read();
    rows.add(attempt.toJson());
    await _store.write(rows);
  }

  Future<int> pending() async => (await _store.read()).length;

  /// Отправляет накопленное.
  ///
  /// Очередь очищается только после того, как сервер ответил: очисти мы её
  /// до ответа — и обрыв стоил бы врачу вечера. Повтор при этом безопасен:
  /// ключ повторности у каждого разбора свой, и второй раз он не ляжет.
  Future<Flushed> flush() async {
    // Негодная строка не уезжает: сервер отбросил бы её молча, а очередь
    // считала бы её отправленной. Разбирается каждая, и отбрасывается
    // только она сама — очередь из-за неё не пропадает.
    final rows = [
      for (final row in await _store.read())
        if (PendingAttempt.tryParse(row) != null) row,
    ];
    if (rows.isEmpty) {
      await _store.write([]);
      return const Flushed(sent: 0, left: 0);
    }

    var sent = 0;
    while (sent < rows.length) {
      final chunk = rows.skip(sent).take(batch).toList();
      try {
        await _api.post('/v1/attempts', {'attempts': chunk});
      } on ApiFailure catch (failure) {
        // Отправленное убираем, остальное оставляем лежать: потерять
        // очередь из-за отказа на второй пачке значит потерять то, что
        // сервер и не видел.
        await _store.write(rows.skip(sent).toList());
        return Flushed(sent: sent, left: rows.length - sent, failure: failure);
      }
      sent += chunk.length;
    }
    await _store.write([]);
    return Flushed(sent: sent, left: 0);
  }
}
