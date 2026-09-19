/// Расписание повторения на устройстве.
///
/// # Зачем местная копия
///
/// Повторение обязано работать без сети: врач занимается в метро, и
/// «сегодня к повторению» ему нужно там же. Сервер в этот момент
/// недостижим.
///
/// # Хозяин расписания — сервер
///
/// Не устройство. У врача может быть второй телефон, и разбор, сделанный
/// на нём, местная копия не видит. Поэтому при расхождении побеждает
/// сервер: его ответ на `/v1/review` кладётся поверх местного, а местное
/// состояние — это то, чем живёт приложение, пока сети нет.
///
/// Расчёт при этом один на оба — `progress/rules.dart`, сверенный с общим
/// эталоном. Второй реализации SM-2 здесь нет и быть не должно.
library;

import 'package:sqflite/sqflite.dart';

import '../progress/rules.dart';

/// Задача в расписании.
class Scheduled {
  const Scheduled({required this.caseId, required this.state});

  final String caseId;
  final ReviewState state;
}

/// Расписание в местной базе.
class Schedule {
  Schedule(this._db);

  final Database _db;

  /// Записывает состояние после ответа.
  ///
  /// [synced] false означает «посчитано здесь и серверу ещё не известно».
  /// Отличать нужно затем, чтобы ответ сервера не затирал разбор, который
  /// до него ещё не доехал: иначе задача, разобранная в метро, вернулась
  /// бы к повторению сразу после выхода на связь.
  Future<void> put(
    String caseId,
    ReviewState state, {
    bool synced = false,
  }) async {
    await _db.insert('review', {
      'case_id': caseId,
      'ease': state.ease,
      'interval_d': state.intervalDays,
      'repetitions': state.repetitions,
      'due_at': state.dueAt?.toUtc().millisecondsSinceEpoch ?? 0,
      'synced': synced ? 1 : 0,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<ReviewState?> state(String caseId) async {
    final rows = await _db.query(
      'review',
      where: 'case_id = ?',
      whereArgs: [caseId],
      limit: 1,
    );
    if (rows.isEmpty) return null;
    return _state(rows.first);
  }

  /// Что просрочено на [now], по возрастанию срока.
  ///
  /// По возрастанию, потому что дольше всех ждавшая задача и забыта
  /// сильнее: показать её последней значит потерять её же.
  Future<List<Scheduled>> due(DateTime now, {int limit = 50}) async {
    final rows = await _db.query(
      'review',
      where: 'due_at > 0 AND due_at <= ?',
      whereArgs: [now.toUtc().millisecondsSinceEpoch],
      orderBy: 'due_at, case_id',
      limit: limit,
    );
    return [
      for (final row in rows)
        Scheduled(caseId: row['case_id'] as String, state: _state(row)),
    ];
  }

  /// Кладёт расписание, пришедшее с сервера.
  ///
  /// Разбор, посчитанный здесь и ещё не уехавший, сервером не затирается:
  /// сервер о нём просто не знает, и его слово тут не старше нашего.
  /// Затри мы такую строку — и задача, разобранная в метро, вернулась бы к
  /// повторению сразу после выхода на связь.
  Future<int> accept(Map<String, DateTime> dueByCase) async {
    var taken = 0;
    final batch = _db.batch();
    for (final entry in dueByCase.entries) {
      final rows = await _db.query(
        'review',
        columns: ['synced'],
        where: 'case_id = ?',
        whereArgs: [entry.key],
        limit: 1,
      );
      if (rows.isNotEmpty && rows.first['synced'] == 0) continue;
      batch.insert('review', {
        'case_id': entry.key,
        'due_at': entry.value.toUtc().millisecondsSinceEpoch,
        'synced': 1,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
      taken++;
    }
    await batch.commit(noResult: true);
    return taken;
  }

  /// Отмечает разборы доехавшими. Зовётся после того, как сервер ответил.
  Future<void> markSynced(Iterable<String> caseIds) async {
    if (caseIds.isEmpty) return;
    final batch = _db.batch();
    for (final id in caseIds) {
      batch.update(
        'review',
        {'synced': 1},
        where: 'case_id = ?',
        whereArgs: [id],
      );
    }
    await batch.commit(noResult: true);
  }

  static ReviewState _state(Map<String, Object?> row) {
    final due = row['due_at'];
    return ReviewState(
      ease: (row['ease'] as num?)?.toDouble() ?? 0,
      intervalDays: (row['interval_d'] as num?)?.toInt() ?? 0,
      repetitions: (row['repetitions'] as num?)?.toInt() ?? 0,
      dueAt: due is int && due > 0
          // Местное время, а не UTC: срок показывается врачу, и час
          // сдвига виден ему как «задача пришла не тогда».
          ? DateTime.fromMillisecondsSinceEpoch(due, isUtc: true).toLocal()
          : null,
    );
  }
}
