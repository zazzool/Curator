/// Очередь разборов в местной базе.
///
/// Переезд из настроек, обещанный в `cases/outbox.dart`. Причина переезда
/// не в красоте: настройки на Android читаются в память ЦЕЛИКОМ при
/// запуске приложения, а очередь за месяц без сети вырастает — и запуск
/// становится тем дольше, чем дольше врач был без связи.
///
/// # Прежняя очередь не выбрасывается, а переносится
///
/// На устройствах, обновившихся с прежней сборки, очередь лежит в
/// настройках, и это чей-то месяц занятий. Первое открытие базы
/// перекладывает её сюда и чистит прежнее место — один раз, потому что
/// после переноса там пусто.
library;

import 'package:sqflite/sqflite.dart';

import '../cases/outbox.dart';

/// Очередь в базе.
class DbOutboxStore implements OutboxStore {
  DbOutboxStore(this._db);

  final Database _db;

  @override
  Future<List<Map<String, dynamic>>> read() async {
    final rows = await _db.query('outbox', orderBy: 'happened_at, idem_key');
    return [
      for (final row in rows)
        {
          'caseId': row['case_id'],
          'correct': row['correct'] == 1,
          'answer': row['answer'],
          'mode': row['mode'],
          'spentMs': row['spent_ms'],
          'idemKey': row['idem_key'],
          'happenedAt': DateTime.fromMillisecondsSinceEpoch(
            row['happened_at'] as int,
            isUtc: true,
          ).toIso8601String(),
        },
    ];
  }

  @override
  Future<void> write(List<Map<String, dynamic>> rows) async {
    // Очередь переписывается целиком: так её видит вызывающий, и
    // раздваивать это правило значит заводить два способа её потерять.
    await _db.transaction((txn) async {
      await txn.delete('outbox');
      for (final row in rows) {
        final key = row['idemKey'];
        final caseId = row['caseId'];
        final at = DateTime.tryParse(
          row['happenedAt'] is String ? row['happenedAt'] as String : '',
        );
        // Негодная строка не ложится: она всё равно не уедет, а место
        // займёт и порядок собьёт.
        if (key is! String || key.isEmpty) continue;
        if (caseId is! String || caseId.isEmpty || at == null) continue;
        await txn.insert('outbox', {
          'idem_key': key,
          'case_id': caseId,
          'correct': row['correct'] == true ? 1 : 0,
          'answer': row['answer'] is String ? row['answer'] : '',
          'mode': row['mode'] is String ? row['mode'] : '',
          'spent_ms': row['spentMs'] is int ? row['spentMs'] : 0,
          'happened_at': at.toUtc().millisecondsSinceEpoch,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
      }
    });
  }

  /// Переносит очередь из прежнего места хранения.
  ///
  /// Зовётся один раз при открытии базы. Прежнее место чистится только
  /// после успешной записи: обрыв между чтением и очисткой не должен
  /// стоить врачу месяца занятий.
  Future<int> adopt(OutboxStore older) async {
    final rows = await older.read();
    if (rows.isEmpty) return 0;
    final mine = await read();
    await write([...mine, ...rows]);
    await older.write([]);
    return rows.length;
  }
}
