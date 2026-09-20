/// Витрина наборов глазами врача.
///
/// Витрина показывает цену и то, открыт ли набор уже сейчас. Витрина, не
/// говорящая, что уже куплено, заставляет врача выяснять это покупкой.
///
/// # Почему «закрыт» здесь никогда не стоит в одиночку
///
/// Закрытый набор без причины — тупик: врач не знает, войти ему, привязать
/// почту, подписаться или ждать оператора, и одинаково часто не делает
/// ничего. Поэтому сервер присылает линейку, а витрина переводит её во
/// «что сделать». Пока линейка сюда не доезжала, всякий закрытый набор
/// обещал, что он «появится, как только будет оплачен», — и врачу,
/// которому достаточно было привязать почту, это было прямой неправдой.
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
    this.line = '',
    this.openedBy = '',
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

  /// Линейка набора: чем он открывается. Словарь закрытый, зеркало
  /// серверного (`PackLine`), и сверяет его проверка сервера.
  ///
  /// Незнакомая линейка — пустая строка: старое приложение получит новую
  /// линейку и не должно из-за неё ни падать, ни врать про способ оплаты.
  final String line;

  /// Чем набор открыт СЕЙЧАС (`OpenedBy`), у закрытого пусто.
  ///
  /// Отдельно от `line`: линейка говорит, чем набор открывается вообще, а
  /// это — чем он открыт у ЭТОГО врача. Купленное не отбирают никогда;
  /// открытое группой держится на правиле, а правило смотрит на живого
  /// врача и может перестать на него сходиться.
  final String openedBy;

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
      line: PackLine.known(raw['line']),
      openedBy: OpenedBy.known(raw['openedBy']),
    );
  }
}

/// Линейки наборов — зеркало серверного словаря (`packs.Lines`).
///
/// Словарь закрытый, и закрытость его — это то, что делает контракт
/// дешёвым: линейка, которой здесь нет, не значит для витрины ничего.
/// Зеркальность стережёт проверка сервера, читающая этот файл построчно:
/// два места для одного словаря расходятся молча.
abstract final class PackLine {
  /// Открыт всякому, без единой строки прав.
  static const guest = 'guest';

  /// Открыт тому, кто привязал почту.
  static const basic = 'basic';

  /// Открыт подпиской или покупкой.
  static const paid = 'paid';

  /// Открыт всем и бесплатен: за него заплатил спонсор.
  static const sponsored = 'sponsored';

  static const all = [guest, basic, paid, sponsored];

  /// Незнакомая линейка отбрасывается целиком, а не подменяется платной:
  /// непонятое не применяется. Витрина промолчит о способе, но не соврёт
  /// про него — а соврала бы, подставь мы сюда умолчание.
  static String known(Object? raw) =>
      raw is String && all.contains(raw) ? raw : '';
}

/// Чем набор открыт — зеркало серверного словаря (`packs.By…`).
abstract final class OpenedBy {
  /// Куплен или выдан оператором. Такое не отбирают.
  static const purchase = 'purchase';

  /// Открыт группе, в которую попадает врач.
  static const group = 'group';

  /// Открыт своей линейкой.
  static const line = 'line';

  static const all = [purchase, group, line];

  static String known(Object? raw) =>
      raw is String && all.contains(raw) ? raw : '';
}

/// Что сделать врачу с закрытым набором — словами и по его линейке.
///
/// Здесь единственное место, где линейка превращается в разговор с
/// врачом, и оно нарочно не хранит текстов рядом с самим словарём:
/// словарь сверяется с сервером построчно, и человеческая строка в нём
/// сверку сломала бы.
///
/// Незнакомая линейка не обещает ничего сверх «пока закрыт»: старое
/// приложение получит однажды линейку, которой не знает, и соврать про
/// способ оплаты хуже, чем промолчать о нём.
String closedNote(String line) {
  switch (line) {
    case PackLine.basic:
      // Почта и есть то, чем «авторизованный» у нас отличается от
      // промолчавшего: запись заводится молча при первом запуске.
      return 'Привяжите почту в настройках — и набор откроется';
    case PackLine.paid:
      // Про кнопку «Купить» здесь не обещается ничего: приём платежей не
      // сделан, право оформляет оператор. Обещание, которого никто не
      // собирается исполнять, отправляет врача ждать.
      return 'Открывается подпиской или покупкой набора';
    default:
      return 'Набор пока закрыт';
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
