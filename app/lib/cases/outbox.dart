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
/// # Где живёт очередь
///
/// В местной базе — `db/outbox_store.dart`. Прежде она жила в настройках,
/// потому что местной базы не было; переезд состоялся, и прежняя очередь
/// переносится при подъёме. Настройки остались хранилищем для проверок и
/// для устройств, не дошедших до переноса.
///
/// # Очередь правится по одному действию за раз
///
/// И `add`, и `flush` — это «прочитать всё, изменить, записать всё»
/// поверх одного хранилища. Без порядка они затирали друг друга: `flush`
/// снимал список, уходил в сеть на двадцать секунд слабой связи, а
/// вернувшись — записывал поверх очереди пустоту вместе с ответами,
/// которые врач дал за эти секунды. Ответ пропадал молча, и счётчик
/// «ждут отправки» показывал ноль.
///
/// Поэтому правки очереди выстроены в очередь сами (`_inTurn`), а
/// обращение к серверу идёт БЕЗ замка: держи мы замок через сеть, и ответ,
/// данный в эти секунды, ждал бы записи столько же — а убитое в это время
/// приложение унесло бы его с собой. Из очереди при этом убираются именно
/// отправленные разборы, по ключам повторности, а не «всё, что лежало».
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

  /// Потолок времени над задачей.
  ///
  /// Часы устройства здесь не источник правды, а мерило промежутка — и
  /// промежуток этот считается от показа до ответа, то есть включает
  /// свёрнутое приложение, уснувший телефон и ночь между ними. Врач,
  /// вернувшийся к задаче наутро, записывал девятичасовой ответ, и он
  /// уезжал в сводку решаемости наравне с настоящими.
  ///
  /// Десять минут: дольше над одной ситуационной задачей не сидят, а
  /// если сидят — числом это уже не про задачу, а про то, что человек
  /// отвлёкся. Обрезанное значение честнее выброшенного: ответ был, и
  /// правильность его известна, а время просто неизмеримо.
  static const maxSpentMs = 10 * 60 * 1000;

  /// Приводит измеренное время к тому, что можно записать.
  ///
  /// Отрицательное — не описка вызывающего: часы устройства переводят
  /// назад, и разность двух «сейчас» бывает меньше нуля.
  static int clampSpent(int measured) {
    if (measured < 0) return 0;
    return measured > maxSpentMs ? maxSpentMs : measured;
  }
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

  /// Хвост очереди правок. Довод — в пояснении к файлу.
  Future<void> _turn = Future.value();

  /// Выполняет правку очереди, дождавшись предыдущей.
  Future<T> _inTurn<T>(Future<T> Function() work) {
    final result = _turn.then((_) => work());
    // Отказ одной правки не рвёт цепочку для следующих: иначе первая же
    // неудачная запись заперла бы очередь навсегда, и врач перестал бы
    // копить разборы, ничего об этом не узнав.
    _turn = result.then((_) {}, onError: (_) {});
    return result;
  }

  Future<void> add(PendingAttempt attempt) => _inTurn(() async {
    final rows = await _store.read();
    rows.add(attempt.toJson());
    await _store.write(rows);
  });

  Future<int> pending() => _inTurn(() async => (await _store.read()).length);

  /// Отправляет накопленное.
  ///
  /// Очередь очищается только после того, как сервер ответил: очисти мы её
  /// до ответа — и обрыв стоил бы врачу вечера. Повтор при этом безопасен:
  /// ключ повторности у каждого разбора свой, и второй раз он не ляжет.
  Future<Flushed> flush() async {
    // Снимок берётся под замком, сеть идёт без него.
    //
    // Негодная строка не уезжает: сервер отбросил бы её молча, а очередь
    // считала бы её отправленной. Разбирается каждая, и отбрасывается
    // только она сама — очередь из-за неё не пропадает. Убираются они
    // здесь же, под замком: иначе остались бы в очереди навсегда, потому
    // что ключа повторности у них нет и убрать их по ключу нечем.
    final rows = await _inTurn(() async {
      final kept = [
        for (final row in await _store.read())
          if (PendingAttempt.tryParse(row) != null) row,
      ];
      await _store.write(kept);
      return kept;
    });
    if (rows.isEmpty) return const Flushed(sent: 0, left: 0);

    var sent = 0;
    ApiFailure? failure;
    while (sent < rows.length) {
      final chunk = rows.skip(sent).take(batch).toList();
      try {
        await _api.post('/v1/attempts', {'attempts': chunk});
      } on ApiFailure catch (denied) {
        // Отправленное убираем ниже, остальное остаётся лежать: потерять
        // очередь из-за отказа на второй пачке значит потерять то, что
        // сервер и не видел.
        failure = denied;
        break;
      }
      sent += chunk.length;
    }

    // Убираются ИМЕННО отправленные, по ключам повторности. Запись поверх
    // очереди пустотой унесла бы с собой ответы, данные врачом, пока шло
    // обращение к серверу, — и унесла бы молча.
    final done = {for (final row in rows.take(sent)) row['idemKey'] as String};
    final left = await _inTurn(() async {
      final rest = [
        for (final row in await _store.read())
          if (!done.contains(row['idemKey'])) row,
      ];
      await _store.write(rest);
      return rest.length;
    });
    return Flushed(sent: sent, left: left, failure: failure);
  }
}
