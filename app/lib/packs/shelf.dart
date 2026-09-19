/// Витрина наборов глазами врача.
///
/// Витрина показывает цену и то, открыт ли набор уже сейчас. Витрина, не
/// говорящая, что уже куплено, заставляет врача выяснять это покупкой.
library;

import '../api/client.dart';
import 'store.dart';

/// Набор в витрине.
class ShelfPack {
  const ShelfPack({
    required this.slug,
    required this.title,
    required this.summaryMd,
    required this.version,
    required this.cases,
    required this.kopecks,
    required this.owned,
    required this.installed,
  });

  final String slug;
  final String title;
  final String summaryMd;

  /// Выпуск, который предлагает сервер.
  final int version;

  final int cases;

  /// Цена в КОПЕЙКАХ, ноль значит «бесплатно».
  ///
  /// Копейки, а не рубли: деньги не едут дробным числом никогда — рубль с
  /// копейками, сложенный тысячу раз в двоичной дроби, расходится с любым
  /// счётом. Рубли — дело показа, и перевод делается при выводе.
  final int kopecks;

  /// Открыт ли набор этому врачу уже сейчас — покупкой, подпиской или
  /// потому, что он бесплатен.
  final bool owned;

  /// Выпуск, лежащий на устройстве. Ноль значит, что набора здесь нет.
  final int installed;

  bool get isInstalled => installed > 0;

  /// На устройстве лежит выпуск постарше того, что предлагает сервер.
  bool get isStale => installed > 0 && installed < version;

  static ShelfPack? tryParse(Object? raw, int installed) {
    if (raw is! Map) return null;
    final slug = raw['slug'];
    final title = raw['title'];
    final version = raw['version'];
    final cases = raw['cases'];
    final kopecks = raw['kopecks'];
    if (slug is! String || slug.isEmpty) return null;
    if (title is! String) return null;
    if (version is! int || cases is! int || kopecks is! int) return null;
    return ShelfPack(
      slug: slug,
      title: title,
      summaryMd: raw['summaryMd'] is String ? raw['summaryMd'] as String : '',
      version: version,
      cases: cases,
      kopecks: kopecks,
      owned: raw['owned'] == true,
      installed: installed,
    );
  }
}

/// Цена рублями — для показа, и только для него.
String rubles(int kopecks) {
  if (kopecks <= 0) return 'Бесплатно';
  final whole = kopecks ~/ 100;
  final rest = kopecks % 100;
  // Копейки показываются, только если они есть: «390 ₽» читается, «390,00 ₽»
  // выглядит бухгалтерской выпиской.
  if (rest == 0) return '$whole ₽';
  return '$whole,${rest.toString().padLeft(2, '0')} ₽';
}

/// Витрина.
class Shelf {
  Shelf(this._api, this._store);

  final Api _api;
  final PackStore _store;

  /// Читает витрину и дополняет её тем, что уже лежит на устройстве.
  ///
  /// Сервер про устройство не знает и знать не должен: что скачано —
  /// сведения о телефоне, а не об учётной записи, и спрашивать их у
  /// сервера значит рассказывать ему то, чего он не спрашивал.
  Future<List<ShelfPack>> list() async {
    final reply = await _api.get('/v1/packs');
    final raw = reply['packs'];
    if (raw is! List) return [];

    final here = <String, int>{};
    for (final slug in await _store.installed()) {
      final one = await _store.release(slug);
      if (one != null) here[slug] = one.version;
    }

    final out = <ShelfPack>[];
    for (final one in raw) {
      // Непонятая строка пропускается, а витрина показывается: один
      // негодный набор не должен оставлять врача с пустым экраном.
      final parsed = ShelfPack.tryParse(
        one,
        one is Map && one['slug'] is String
            ? (here[one['slug'] as String] ?? 0)
            : 0,
      );
      if (parsed != null) out.add(parsed);
    }
    return out;
  }
}
