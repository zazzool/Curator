/// Лента задач: что качать и что показывать.
library;

import '../api/client.dart';
import 'model.dart';

/// Страница ленты.
class FeedPage {
  const FeedPage({
    required this.cases,
    required this.next,
    required this.version,
    required this.dropped,
  });

  final List<CaseItem> cases;

  /// Курсор следующей страницы. Пусто — страниц больше нет.
  final String next;

  /// Версия содержания на момент выдачи страницы. Меняется — значит,
  /// посреди обхода случился выпуск, и обход надо начинать заново.
  final int version;

  /// Сколько задач страницы не разобралось. Считается и не прячется:
  /// молча выброшенная половина ленты выглядит как «задач больше нет», а
  /// это разные беды с разным лечением.
  final int dropped;
}

/// Лента.
class Feed {
  Feed(this._api);

  final Api _api;

  /// Качает страницу.
  ///
  /// Страницами по курсору, а не по смещению: между двумя страницами
  /// задачу могут выпустить, и при смещении она сдвинет все следующие —
  /// врач получит одну задачу дважды, а соседнюю не получит вовсе.
  ///
  /// Битая задача выбрасывается, а страница отдаётся: одна такая задача не
  /// должна стоить врачу всей ленты.
  Future<FeedPage> page({
    String after = '',
    String path = '',
    int limit = 0,
  }) async {
    final reply = await _api.get(
      '/v1/cases',
      query: {
        if (after.isNotEmpty) 'after': after,
        if (path.isNotEmpty) 'path': path,
        if (limit > 0) 'limit': '$limit',
      },
    );

    final rows = reply['cases'];
    final cases = <CaseItem>[];
    var dropped = 0;
    if (rows is List) {
      for (final row in rows) {
        final one = row is Map<String, dynamic> ? CaseItem.tryParse(row) : null;
        if (one == null) {
          dropped++;
          continue;
        }
        cases.add(one);
      }
    }

    return FeedPage(
      cases: cases,
      next: reply['next'] is String ? reply['next'] as String : '',
      version: reply['version'] is int ? reply['version'] as int : 0,
      dropped: dropped,
    );
  }
}
