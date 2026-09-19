/// Лента задач: что качать и что показывать.
///
/// # Без сети лента берётся из скачанных наборов
///
/// Ради этого наборы и ставятся на устройство. Пока лента ходила только к
/// серверу, скачанный набор лежал мёртвым грузом: витрина говорила
/// «работает без сети», а в метро врач видел отказ. Поэтому отказ ИМЕННО
/// по отсутствию сети переводит ленту на лежащее рядом.
///
/// Отказ сервера так не переводит: «войдите заново» и «нет сети» — разные
/// беды, и подмени мы первую задачами из набора, врач занимался бы, не
/// зная, что его разборы никуда не уходят.
library;

import '../api/client.dart';
import '../packs/store.dart';
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
  Feed(this._api, [this._packs]);

  final Api _api;

  /// Наборы на устройстве. Без них лента просто не имеет запасного пути.
  final PackStore? _packs;

  /// Что сегодня повторять.
  ///
  /// Содержание задачи едет целиком, а не одним номером: повторение обязано
  /// работать без сети, а без сети некуда сходить за содержанием.
  ///
  /// Пустой список — исправный случай и самый частый из всех: «сегодня
  /// нечего повторять» не отказ.
  Future<List<CaseItem>> due() async {
    final reply = await _api.get('/v1/review');
    final rows = reply['cases'];
    if (rows is! List) return [];
    final out = <CaseItem>[];
    for (final row in rows) {
      if (row is! Map<String, dynamic>) continue;
      final one = CaseItem.tryParse(row);
      if (one != null) out.add(one);
    }
    return out;
  }

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
    try {
      return await _fromServer(after: after, path: path, limit: limit);
    } on ApiFailure catch (failure) {
      final packs = _packs;
      if (!failure.offline || packs == null) rethrow;
      final local = await _fromPacks(packs, after: after, limit: limit);
      // Наборов нет — значит, запасного пути и правда нет, и врачу надо
      // сказать про сеть, а не показать пустую ленту: пустая лента
      // читается как «задачи кончились».
      if (local.cases.isEmpty && after.isEmpty) rethrow;
      return local;
    }
  }

  Future<FeedPage> _fromServer({
    required String after,
    required String path,
    required int limit,
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

  /// Лента из того, что лежит на устройстве.
  ///
  /// Курсор ходит по номеру задачи, как и на сервере, и поэтому страницы
  /// здесь и там нарезаются одинаково. Задачи берутся только из наборов,
  /// поставленных ЦЕЛИКОМ: у недокачанного набора описи нет, и показывать
  /// его куски значит выдавать половину за целое.
  ///
  /// Название единицы источника здесь пустое: сервер отдаёт в наборе
  /// только номер и содержание, а выдумывать название нельзя — врач
  /// прочтёт выдуманное как настоящее.
  Future<FeedPage> _fromPacks(
    PackStore packs, {
    required String after,
    required int limit,
  }) async {
    final take = limit > 0 ? limit : 20;
    final ids = <String, String>{}; // номер задачи -> набор
    for (final slug in await packs.installed()) {
      for (final id in await packs.have(slug)) {
        ids.putIfAbsent(id, () => slug);
      }
    }
    final order = ids.keys.toList()..sort();

    final cases = <CaseItem>[];
    var dropped = 0;
    var next = '';
    var last = '';
    for (final id in order) {
      if (after.isNotEmpty && id.compareTo(after) <= 0) continue;
      if (cases.length + dropped >= take) {
        // Курсор — номер последней ОТДАННОЙ задачи, а не первой
        // неотданной: сервер продолжает по строгому «больше чем», и
        // второе правило потеряло бы ровно одну задачу на каждой границе
        // страниц. Потерю такого рода никто не замечает.
        next = last;
        break;
      }
      last = id;
      final body = await packs.caseBody(ids[id]!, id);
      final one = body is Map<String, dynamic>
          ? CaseItem.tryParse({'id': id, 'body': body})
          : null;
      if (one == null) {
        // Считается и не прячется — ровно как у ленты с сервера.
        dropped++;
        continue;
      }
      cases.add(one);
    }

    // Версия нулевая: выпуск на сервере отсюда не виден, и подставить
    // сюда версию набора значит сказать неправду о другом числе.
    return FeedPage(cases: cases, next: next, version: 0, dropped: dropped);
  }
}
