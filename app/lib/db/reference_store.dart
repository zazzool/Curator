/// Справочник на устройстве.
///
/// # Зачем местная копия
///
/// Ради неё справочник и переносится: врач открывает рубрику в отделении,
/// где сети нет, и критерии должны быть там же. Копия, за которой надо
/// сходить в сеть, — это не справочник, а обещание справочника.
///
/// # Копия заменяется целиком, а не правится построчно
///
/// Сервер отдаёт справочник выпусками, и досылка «что изменилось» была бы
/// дешевле по трафику и дороже по правде: удалённую рубрику досылать
/// нечем, и копия накапливала бы то, чего в источнике уже нет. Поэтому
/// новый выпуск кладётся вместо старого, одной транзакцией: оборвись
/// закачка посередине — на устройстве остаётся прежний целый выпуск, а не
/// половина нового.
library;

import 'package:sqflite/sqflite.dart';

import '../reference/model.dart';

/// Справочник в местной базе.
class ReferenceStore {
  ReferenceStore(this._db);

  final Database _db;

  /// Какие источники лежат на устройстве.
  Future<List<RefSource>> sources() async {
    final rows = await _db.query('ref_sources', orderBy: 'title');
    return rows.map(_sourceOf).toList();
  }

  /// Выпуск лежащей копии. Ноль — копии нет вовсе.
  Future<int> versionOf(String slug) async {
    final rows = await _db.query(
      'ref_sources',
      columns: ['version'],
      where: 'slug = ?',
      whereArgs: [slug],
      limit: 1,
    );
    if (rows.isEmpty) return 0;
    return (rows.first['version'] as int?) ?? 0;
  }

  /// Кладёт выпуск целиком, заменяя прежний.
  ///
  /// Одной транзакцией — вместе с уборкой прежнего. Разбей это на два
  /// шага, и обрыв между ними оставил бы врача без справочника: старого
  /// уже нет, нового ещё нет.
  Future<void> replace(
    RefSource source,
    List<RefUnit> units,
    List<RefStatement> statements, {
    required int syncedAt,
  }) async {
    await _db.transaction((tx) async {
      await tx.delete(
        'ref_units',
        where: 'source_slug = ?',
        whereArgs: [source.slug],
      );
      await tx.delete(
        'ref_statements',
        where: 'source_slug = ?',
        whereArgs: [source.slug],
      );

      await tx.insert('ref_sources', {
        'slug': source.slug,
        'title': source.title,
        'unit_word': source.unitWord,
        'statement_word': source.statementWord,
        'edition': source.edition,
        'version': source.version,
        'units': units.length,
        'statements': statements.length,
        'synced_at': syncedAt,
      }, conflictAlgorithm: ConflictAlgorithm.replace);

      // Пачками, а не по строке: у справочника рубрик тысячи, и тысяча
      // отдельных вставок на телефоне — это секунды ожидания на экране.
      final batch = tx.batch();
      for (final u in units) {
        batch.insert('ref_units', {
          'source_slug': source.slug,
          'label': u.label,
          'parent_label': u.parentLabel,
          'title': u.title,
          'path': u.path,
          'depth': u.depth,
          'kind': u.kind,
          'answerable': u.answerable ? 1 : 0,
          'statements': u.statements,
          'ord': u.ord,
          'search': searchText(u),
        }, conflictAlgorithm: ConflictAlgorithm.replace);
      }
      for (final s in statements) {
        batch.insert('ref_statements', {
          'id': s.id,
          'source_slug': source.slug,
          'unit_label': s.unitLabel,
          'kind': s.kind,
          'designation': s.designation,
          'place_ref': s.placeRef,
          'body_md': s.body,
          'ord': s.ord,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
      }
      await batch.commit(noResult: true);
    });
  }

  /// Дети единицы. Пустая метка — корень источника.
  Future<List<RefUnit>> children(String slug, String parentLabel) async {
    final rows = await _db.query(
      'ref_units',
      where: 'source_slug = ? AND parent_label = ?',
      whereArgs: [slug, parentLabel],
      orderBy: 'ord, label',
    );
    return rows.map(_unitOf).toList();
  }

  /// Одна единица по метке.
  Future<RefUnit?> unit(String slug, String label) async {
    final rows = await _db.query(
      'ref_units',
      where: 'source_slug = ? AND label = ?',
      whereArgs: [slug, label],
      limit: 1,
    );
    if (rows.isEmpty) return null;
    return _unitOf(rows.first);
  }

  /// Положения единицы по порядку.
  Future<List<RefStatement>> statements(String slug, String unitLabel) async {
    final rows = await _db.query(
      'ref_statements',
      where: 'source_slug = ? AND unit_label = ?',
      whereArgs: [slug, unitLabel],
      orderBy: 'ord, id',
    );
    return rows.map(_statementOf).toList();
  }

  /// Поиск по метке и названию.
  ///
  /// Поиск ищет и по метке, и по названию, потому что врач приходит с
  /// обоими: с кодом из карты и со словом из головы. Совпадение по началу
  /// строки идёт первым — набравший «F32» хочет F32, а не первую
  /// попавшуюся рубрику, где эти знаки встретились в середине; строка
  /// начинается с метки, поэтому начало строки и есть начало метки.
  Future<List<RefUnit>> search(
    String slug,
    String query, {
    int limit = 50,
  }) async {
    final trimmed = query.trim().toLowerCase();
    if (trimmed.isEmpty) return const [];
    final like = '%${_escapeLike(trimmed)}%';
    final prefix = '${_escapeLike(trimmed)}%';
    final rows = await _db.rawQuery(
      '''
      SELECT * FROM ref_units
       WHERE source_slug = ? AND search LIKE ? ESCAPE '\\'
       ORDER BY CASE WHEN search LIKE ? ESCAPE '\\' THEN 0 ELSE 1 END,
                ord, label
       LIMIT ?
      ''',
      [slug, like, prefix, limit],
    );
    return rows.map(_unitOf).toList();
  }

  /// Убирает источник с устройства целиком.
  Future<void> forget(String slug) async {
    await _db.transaction((tx) async {
      await tx.delete('ref_units', where: 'source_slug = ?', whereArgs: [slug]);
      await tx.delete(
        'ref_statements',
        where: 'source_slug = ?',
        whereArgs: [slug],
      );
      await tx.delete('ref_sources', where: 'slug = ?', whereArgs: [slug]);
    });
  }

  RefSource _sourceOf(Map<String, Object?> row) => RefSource(
    slug: row['slug'] as String,
    title: (row['title'] as String?) ?? '',
    unitWord: (row['unit_word'] as String?) ?? '',
    statementWord: (row['statement_word'] as String?) ?? '',
    edition: (row['edition'] as String?) ?? '',
    units: (row['units'] as int?) ?? 0,
    statements: (row['statements'] as int?) ?? 0,
    version: (row['version'] as int?) ?? 0,
    syncedAt: (row['synced_at'] as int?) ?? 0,
  );

  RefUnit _unitOf(Map<String, Object?> row) => RefUnit(
    label: row['label'] as String,
    parentLabel: (row['parent_label'] as String?) ?? '',
    title: (row['title'] as String?) ?? '',
    path: (row['path'] as String?) ?? '',
    depth: (row['depth'] as int?) ?? 0,
    kind: (row['kind'] as String?) ?? 'entry',
    answerable: ((row['answerable'] as int?) ?? 1) != 0,
    statements: (row['statements'] as int?) ?? 0,
    ord: (row['ord'] as int?) ?? 0,
  );

  RefStatement _statementOf(Map<String, Object?> row) => RefStatement(
    id: (row['id'] as int?) ?? 0,
    unitLabel: (row['unit_label'] as String?) ?? '',
    kind: (row['kind'] as String?) ?? '',
    designation: (row['designation'] as String?) ?? '',
    placeRef: (row['place_ref'] as String?) ?? '',
    body: (row['body_md'] as String?) ?? '',
    ord: (row['ord'] as int?) ?? 0,
  );
}

/// Строка, по которой единица ищется.
///
/// Метка первой, потом название, всё в нижнем регистре. Собирается здесь,
/// а не считается запросом: LIKE и LOWER() в SQLite сворачивают регистр
/// только у латиницы, и поиск по-русски молча не находил ничего.
String searchText(RefUnit unit) => '${unit.label} ${unit.title}'.toLowerCase();

/// Обезвреживает знаки подстановки в поисковой строке.
///
/// Без этого «%» из запроса совпадает со всем подряд, и поиск отвечает
/// всем справочником на одну опечатку.
String _escapeLike(String raw) =>
    raw.replaceAll('\\', '\\\\').replaceAll('%', '\\%').replaceAll('_', '\\_');
