/// Справочник, каким его видит устройство.
///
/// # Незнакомое отбрасывается, а остальное принимается
///
/// Ровно то же правило, что у задач и у пачек телеметрии: сервер
/// обновляется без приложения, и новое поле в ответе не должно ронять
/// сборку, стоящую на руках. Отброшенное при этом **считается**: молча
/// выброшенная половина справочника выглядит как «столько в нём и было»,
/// и объяснить это врачу будет нечем.
library;

/// Источник в списке доступного офлайн.
class RefSource {
  const RefSource({
    required this.slug,
    required this.title,
    required this.unitWord,
    required this.statementWord,
    required this.edition,
    required this.units,
    required this.statements,
    required this.version,
    this.syncedAt = 0,
  });

  final String slug;
  final String title;

  /// Как звать единицу и положение у ЭТОГО источника: «рубрика» и
  /// «критерий» у МКБ-10, «пункт» и «положение» у приказа. Показывается
  /// врачу как есть — слово «единица» на экране означало бы, что
  /// приложение не знает, что показывает.
  final String unitWord;
  final String statementWord;

  final String edition;

  /// Размер закачки, названный до неё.
  final int units;
  final int statements;

  /// Выпуск. Сверяется на равенство: копия, отставшая или забежавшая
  /// вперёд, одинаково подлежит замене.
  final int version;

  /// Когда копия легла на устройство. Ноль у того, что приехало с
  /// сервера: сервер о нашей копии не знает и знать не должен.
  final int syncedAt;

  static RefSource? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final slug = raw['slug'];
    final title = raw['title'];
    if (slug is! String || slug.isEmpty) return null;
    if (title is! String || title.isEmpty) return null;
    return RefSource(
      slug: slug,
      title: title,
      unitWord: _text(raw['unitWord'], 'единица'),
      statementWord: _text(raw['statementWord'], 'положение'),
      edition: _text(raw['edition'], ''),
      units: _count(raw['units']),
      statements: _count(raw['statements']),
      version: _count(raw['version']),
    );
  }
}

/// Единица справочника: рубрика, пункт, раздел.
class RefUnit {
  const RefUnit({
    required this.label,
    required this.parentLabel,
    required this.title,
    required this.path,
    required this.depth,
    required this.kind,
    required this.answerable,
    required this.statements,
    required this.ord,
  });

  final String label;
  final String parentLabel;
  final String title;

  /// Материализованная цепочка предков ('F30-F39/F32/F32.1'). По ней
  /// строится и навигация вглубь, и «где я нахожусь».
  final String path;
  final int depth;

  /// Род записи, как его назвал сам источник: 'group' — вход в навигацию,
  /// 'entry' — то, по чему спрашивают. Спрашивается у источника, а не
  /// угадывается по формату метки: «если это МКБ» — дефект.
  final String kind;

  /// Пригодна ли к ответу. У групп — нет: группа это вход в навигацию, а
  /// не то, что спрашивают.
  final bool answerable;

  /// Сколько у неё положений. Список с пометкой «тут есть критерии»
  /// читается иначе, чем список без неё.
  final int statements;

  final int ord;

  static RefUnit? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final label = raw['label'];
    if (label is! String || label.isEmpty) return null;
    return RefUnit(
      label: label,
      parentLabel: _text(raw['parentLabel'], ''),
      title: _text(raw['title'], ''),
      path: _text(raw['path'], label),
      depth: _count(raw['depth']),
      kind: _text(raw['kind'], 'entry'),
      answerable: raw['answerable'] is bool ? raw['answerable'] as bool : true,
      statements: _count(raw['statements']),
      ord: _count(raw['ord']),
    );
  }
}

/// Положение единицы: критерий, пункт, абзац.
class RefStatement {
  const RefStatement({
    required this.id,
    required this.unitLabel,
    required this.kind,
    required this.designation,
    required this.placeRef,
    required this.body,
    required this.ord,
  });

  final int id;
  final String unitLabel;
  final String kind;

  /// Обозначение в первоисточнике («G1», «абз. 2») и ссылка на место
  /// (страница, пункт). Пустые, если их нет: выдуманная ссылка на место
  /// хуже отсутствующей — по ней врач пойдёт проверять и не найдёт.
  final String designation;
  final String placeRef;

  final String body;
  final int ord;

  static RefStatement? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final id = raw['id'];
    final unit = raw['unitLabel'];
    final body = raw['bodyMd'];
    if (id is! num || unit is! String || unit.isEmpty) return null;
    if (body is! String || body.isEmpty) return null;
    return RefStatement(
      id: id.toInt(),
      unitLabel: unit,
      kind: _text(raw['kind'], ''),
      designation: _text(raw['designation'], ''),
      placeRef: _text(raw['placeRef'], ''),
      body: body,
      ord: _count(raw['ord']),
    );
  }
}

/// Разобранная страница: что удалось прочесть и сколько выброшено.
class RefPage<T> {
  const RefPage({
    required this.items,
    required this.next,
    required this.version,
    required this.dropped,
  });

  final List<T> items;

  /// Курсор следующей страницы. Пусто — страниц больше нет.
  final String next;

  /// Выпуск источника на момент выдачи страницы. Изменился посреди
  /// обхода — копия собралась бы из двух выпусков, причём выглядела бы
  /// целой, и обход надо начинать заново.
  final int version;

  /// Сколько строк не разобралось. Считается и не прячется.
  final int dropped;
}

RefPage<RefUnit> parseUnitsPage(Map<String, dynamic> raw) =>
    _page(raw, 'units', RefUnit.tryParse);

RefPage<RefStatement> parseStatementsPage(Map<String, dynamic> raw) =>
    _page(raw, 'statements', RefStatement.tryParse);

List<RefSource> parseSources(Map<String, dynamic> raw) {
  final list = raw['sources'];
  if (list is! List) return const [];
  final out = <RefSource>[];
  for (final one in list) {
    final parsed = RefSource.tryParse(one);
    if (parsed != null) out.add(parsed);
  }
  return out;
}

RefPage<T> _page<T>(
  Map<String, dynamic> raw,
  String field,
  T? Function(Object?) parse,
) {
  final list = raw[field];
  final items = <T>[];
  var dropped = 0;
  if (list is List) {
    for (final one in list) {
      final parsed = parse(one);
      if (parsed == null) {
        dropped++;
        continue;
      }
      items.add(parsed);
    }
  }
  return RefPage<T>(
    items: items,
    next: _text(raw['next'], ''),
    version: _count(raw['version']),
    dropped: dropped,
  );
}

String _text(Object? raw, String fallback) =>
    raw is String && raw.isNotEmpty ? raw : fallback;

int _count(Object? raw) => raw is num ? raw.toInt() : 0;
