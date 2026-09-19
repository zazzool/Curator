/// Прогресс и знаки, какими их видит приложение.
///
/// # Знаки считает сервер, а не устройство
///
/// Номер, дата выдачи, тираж и доля обладателей — сведения о том, каким
/// врач пришёл среди всех, и вывести их из его собственной истории нельзя.
/// Приложение их только показывает. Само оно считает ровно одно — долю
/// пути к знаку, и то лишь затем, чтобы она была видна без сети.
library;

import '../api/client.dart';
import 'metrics.dart';

/// Опыт, уровень и величины.
class Standing {
  const Standing({
    required this.xp,
    required this.level,
    required this.due,
    required this.metrics,
  });

  final int xp;
  final int level;

  /// Сколько задач ждёт повторения прямо сейчас, а не всего назначено.
  final int due;

  /// Величины каталога — всегда полным набором, с нулями у тех, до
  /// которых врач ещё не дошёл.
  final Map<String, int> metrics;

  static const empty = Standing(xp: 0, level: 1, due: 0, metrics: {});

  static Standing parse(Map<String, dynamic> row) => Standing(
    xp: _int(row['xp']),
    level: _int(row['level']) == 0 ? 1 : _int(row['level']),
    due: _int(row['due']),
    metrics: Metrics.fill(
      row['metrics'] is Map<String, dynamic>
          ? row['metrics'] as Map<String, dynamic>
          : const {},
    ),
  );
}

/// Знак глазами врача.
class SignStanding {
  const SignStanding({
    required this.slug,
    required this.title,
    required this.kind,
    required this.issued,
    required this.progress,
    this.serial = 0,
    this.issuedAt = '',
    this.revoked = false,
    this.issuedCount = 0,
    this.editionSize = 0,
    this.holdersShare = 0,
  });

  final String slug;
  final String title;
  final String kind;

  final bool issued;

  /// Доля пути от 0 до 1. У выданного — единица.
  final double progress;

  final int serial;
  final String issuedAt;

  /// Только у переходящего знака. Строка о выдаче не удаляется никогда,
  /// поэтому отозванный знак остаётся в списке с отметкой, а не исчезает.
  final bool revoked;

  final int issuedCount;

  /// Ноль значит «тираж не ограничен».
  final int editionSize;

  final double holdersShare;

  static SignStanding? tryParse(Map<String, dynamic> row) {
    final slug = row['slug'];
    final title = row['title'];
    if (slug is! String || slug.isEmpty || title is! String || title.isEmpty) {
      // Знак без метки или названия нечем ни показать, ни отличить от
      // соседнего. Отбрасывается он один, а не весь список.
      return null;
    }
    return SignStanding(
      slug: slug,
      title: title,
      kind: row['kind'] is String ? row['kind'] as String : '',
      issued: row['issued'] == true,
      progress: _double(row['progress']).clamp(0, 1),
      serial: _int(row['serial']),
      issuedAt: row['issuedAt'] is String ? row['issuedAt'] as String : '',
      revoked: row['revoked'] == true,
      issuedCount: _int(row['issuedCount']),
      editionSize: _int(row['editionSize']),
      holdersShare: _double(row['holdersShare']),
    );
  }
}

/// Чтение прогресса и знаков с сервера.
class Standings {
  Standings(this._api);

  final Api _api;

  Future<Standing> standing() async =>
      Standing.parse(await _api.get('/v1/progress'));

  /// Каталог знаков целиком, вместе с невыданными: знак, о котором врач не
  /// знает, не мотивирует никого.
  Future<List<SignStanding>> signs() async {
    final reply = await _api.get('/v1/signs');
    final rows = reply['signs'];
    if (rows is! List) return [];
    final out = <SignStanding>[];
    for (final row in rows) {
      if (row is! Map<String, dynamic>) continue;
      final one = SignStanding.tryParse(row);
      if (one != null) out.add(one);
    }
    return out;
  }
}

int _int(Object? value) => switch (value) {
  final int v => v,
  final double v => v.round(),
  _ => 0,
};

double _double(Object? value) => switch (value) {
  final int v => v.toDouble(),
  final double v => v,
  _ => 0,
};
