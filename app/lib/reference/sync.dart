/// Закачка справочника на устройство.
///
/// # Выпуск целиком, а не построчные изменения
///
/// Сервер отдаёт справочник страницами и называет выпуск на каждой из
/// них. Обход собирает выпуск целиком и кладёт его одной транзакцией
/// вместо прежнего. Изменись выпуск посреди обхода — собранное
/// выбрасывается и обход начинается заново: копия из двух выпусков
/// выглядит целой, а таковой не является, и заметить это нечем.
///
/// # Ничего не качается само
///
/// Закачку начинает врач, и начинает зная размер: справочник это
/// мегабайты, а телефон бывает на дорогом тарифе. Молча потраченный
/// трафик — та же молчаливая трата, что и молча выброшенные данные.
library;

import '../api/client.dart';
import '../db/reference_store.dart';
import 'model.dart';

/// Что случилось с закачкой.
class SyncReport {
  const SyncReport({
    required this.slug,
    required this.version,
    required this.units,
    required this.statements,
    required this.dropped,
    required this.restarted,
    this.skipped = false,
  });

  final String slug;
  final int version;
  final int units;
  final int statements;

  /// Сколько строк не разобралось. Считается и показывается: молча
  /// выброшенная половина справочника выглядит как «столько в нём и
  /// было».
  final int dropped;

  /// Сколько раз обход начинался заново из-за выпуска, сменившегося
  /// посреди закачки.
  final int restarted;

  /// Выпуск на устройстве и на сервере совпали, и качать было нечего.
  final bool skipped;
}

/// Закачка справочника.
class ReferenceSync {
  ReferenceSync(this._api, this._store, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final Api _api;
  final ReferenceStore _store;
  final DateTime Function() _now;

  /// Сколько раз обход начинается заново, прежде чем сдаться.
  ///
  /// Три — не суеверие: выпуск меняется приёмкой разбора, и совпасть с
  /// обходом он может раз, ну два. Бесконечный цикл на занятом сервере
  /// врач увидел бы как вечную закачку, и это хуже честного отказа.
  static const restartLimit = 3;

  /// Что сервер предлагает скачать.
  Future<List<RefSource>> available() async {
    final raw = await _api.get('/v1/reference');
    return parseSources(raw);
  }

  /// Качает источник, если лежащая копия отстала.
  ///
  /// [force] заставляет перекачать при совпавшем выпуске: нужно, когда
  /// копия испорчена, а число говорит, что всё в порядке.
  ///
  /// [onProgress] зовётся после каждой страницы и получает, сколько уже
  /// прочитано. Закачка справочника это десятки страниц, и экран без
  /// движущегося числа врач читает как зависший.
  Future<SyncReport> pull(
    RefSource source, {
    bool force = false,
    void Function(int units, int statements)? onProgress,
  }) async {
    if (!force && await _store.versionOf(source.slug) == source.version) {
      return SyncReport(
        slug: source.slug,
        version: source.version,
        units: source.units,
        statements: source.statements,
        dropped: 0,
        restarted: 0,
        skipped: true,
      );
    }

    var restarted = 0;
    while (true) {
      final units = <RefUnit>[];
      final statements = <RefStatement>[];
      var dropped = 0;
      var version = 0;
      var stale = false;

      Future<bool> walk<T>(
        String path,
        RefPage<T> Function(Map<String, dynamic>) parse,
        void Function(List<T>) collect,
      ) async {
        var after = '';
        while (true) {
          final raw = await _api.get(
            path,
            query: {'limit': '200', if (after.isNotEmpty) 'after': after},
          );
          final page = parse(raw);
          if (version == 0) {
            version = page.version;
          } else if (page.version != version) {
            // Выпуск сменился посреди обхода: собранное относится к двум
            // разным справочникам, и склеивать их нельзя.
            return false;
          }
          collect(page.items);
          dropped += page.dropped;
          onProgress?.call(units.length, statements.length);
          if (page.next.isEmpty) return true;
          after = page.next;
        }
      }

      final slug = Uri.encodeComponent(source.slug);
      if (!await walk(
        '/v1/reference/$slug/units',
        parseUnitsPage,
        units.addAll,
      )) {
        stale = true;
      }
      if (!stale &&
          !await walk(
            '/v1/reference/$slug/statements',
            parseStatementsPage,
            statements.addAll,
          )) {
        stale = true;
      }

      if (stale) {
        restarted++;
        if (restarted >= restartLimit) {
          throw ApiFailure(
            'Справочник обновляется прямо сейчас. Попробуйте через минуту.',
          );
        }
        continue;
      }

      await _store.replace(
        RefSource(
          slug: source.slug,
          title: source.title,
          unitWord: source.unitWord,
          statementWord: source.statementWord,
          edition: source.edition,
          units: units.length,
          statements: statements.length,
          version: version,
        ),
        units,
        statements,
        syncedAt: _now().millisecondsSinceEpoch,
      );

      return SyncReport(
        slug: source.slug,
        version: version,
        units: units.length,
        statements: statements.length,
        dropped: dropped,
        restarted: restarted,
      );
    }
  }
}
